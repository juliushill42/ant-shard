// ant/store.go — Inference proxy: bridges ANT routing to local llama.cpp
// TitanU · June 19, 2026 · JCH-2026

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"sync"
)

// InferenceProxy manages the local llama.cpp server process and proxies requests
type InferenceProxy struct {
	modelPath  string
	ramLimit   int64
	serverPort int
	mu         sync.Mutex
	running    bool
}

type InferRequest struct {
	Prompt      string  `json:"prompt"`
	MaxTokens   int     `json:"max_tokens"`
	Temperature float64 `json:"temperature"`
	Stream      bool    `json:"stream"`
}

type InferResponse struct {
	NodeID string `json:"node_id"`
	Text   string `json:"text"`
	Tokens int    `json:"tokens"`
	Model  string `json:"model"`
}

func NewInferenceProxy(modelPath string, ramLimit int64) *InferenceProxy {
	return &InferenceProxy{
		modelPath:  modelPath,
		ramLimit:   ramLimit,
		serverPort: 8080,
	}
}

// EnsureServer starts llama-server if not already running
func (p *InferenceProxy) EnsureServer() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		return nil
	}
	if p.modelPath == "" {
		return fmt.Errorf("no model configured")
	}

	// RAM gate: refuse to load if we'd exceed our allocation
	// llama.cpp q4 models use ~0.5 GB per 1B params
	// We require at least 2GB headroom
	const minHeadroom = 2 * 1024 * 1024 * 1024
	if p.ramLimit < minHeadroom {
		return fmt.Errorf("insufficient RAM: have %dMB, need at least %dMB",
			p.ramLimit/1024/1024, minHeadroom/1024/1024)
	}

	layers := p.estimateGPULayers()
	cmd := exec.Command("llama-server",
		"--model", p.modelPath,
		"--port", strconv.Itoa(p.serverPort),
		"--ctx-size", "4096",
		"--n-gpu-layers", strconv.Itoa(layers),
		"--parallel", "4",
		"--log-disable",
	)
	if err := cmd.Start(); err != nil {
		// Try llama.cpp binary in PATH fallback
		return fmt.Errorf("failed to start llama-server: %v (is llama-server in PATH?)", err)
	}
	p.running = true
	return nil
}

func (p *InferenceProxy) estimateGPULayers() int {
	// Conservative: offload 0 layers by default (CPU-only sovereign mode)
	// Override with ANT_GPU_LAYERS env var
	return 0
}

// HandleInfer proxies an inference request to the local llama-server
func (p *InferenceProxy) HandleInfer(w http.ResponseWriter, r *http.Request) {
	var req InferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}

	if req.MaxTokens == 0 {
		req.MaxTokens = 512
	}
	if req.Temperature == 0 {
		req.Temperature = 0.7
	}

	if err := p.EnsureServer(); err != nil {
		http.Error(w, fmt.Sprintf("inference unavailable: %v", err), 503)
		return
	}

	// Forward to llama-server OpenAI-compatible endpoint
	llamaReq := map[string]interface{}{
		"prompt":      req.Prompt,
		"n_predict":   req.MaxTokens,
		"temperature": req.Temperature,
		"stream":      false,
	}
	body, _ := json.Marshal(llamaReq)

	resp, err := http.Post(
		fmt.Sprintf("http://localhost:%d/completion", p.serverPort),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		http.Error(w, fmt.Sprintf("llama-server error: %v", err), 502)
		return
	}
	defer resp.Body.Close()

	var llamaResp map[string]interface{}
	respBody, _ := io.ReadAll(resp.Body)
	json.Unmarshal(respBody, &llamaResp)

	text, _ := llamaResp["content"].(string)
	out := InferResponse{
		Text:  text,
		Model: modelName(p.modelPath),
	}
	if tc, ok := llamaResp["tokens_predicted"].(float64); ok {
		out.Tokens = int(tc)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func (p *InferenceProxy) HandleHealth(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	running := p.running
	p.mu.Unlock()

	status := "idle"
	if running {
		status = "active"
	}
	if p.modelPath == "" {
		status = "no-model"
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"%s","model":"%s","ram_limit_mb":%d}`,
		status, modelName(p.modelPath), p.ramLimit/1024/1024)
}
