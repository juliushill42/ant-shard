// ant/registry.go — Node registry, heartbeat, capability tracking
// TitanU · June 19, 2026 · JCH-2026

package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

// Registry is the authoritative node directory for one ANT mesh segment.
// Any node can run a registry; they gossip to stay consistent.
type Registry struct {
	mu    sync.RWMutex
	nodes map[string]*NodeInfo
}

func NewRegistry() *Registry {
	return &Registry{nodes: make(map[string]*NodeInfo)}
}

func (r *Registry) Register(n NodeInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n.LastSeen = time.Now()
	r.nodes[n.ID] = &n
	log.Printf("[registry] registered node %s @ %s ram=%dMB model=%s",
		n.ID, n.Addr, n.RAMFree/1024/1024, n.ModelLoaded)
}

func (r *Registry) Heartbeat(id string, ramFree int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n, ok := r.nodes[id]; ok {
		n.LastSeen = time.Now()
		n.RAMFree = ramFree
	}
}

func (r *Registry) Evict(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.nodes, id)
}

func (r *Registry) Sweep() {
	r.mu.Lock()
	defer r.mu.Unlock()
	cutoff := time.Now().Add(-45 * time.Second)
	for id, n := range r.nodes {
		if n.LastSeen.Before(cutoff) {
			log.Printf("[registry] evicting stale node %s", id)
			delete(r.nodes, id)
		}
	}
}

// BestForModel returns the node best suited to run the given model.
// Prefers: already loaded > most free RAM > lowest latency.
func (r *Registry) BestForModel(model string) *NodeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var loaded, best *NodeInfo
	for _, n := range r.nodes {
		if n.ModelLoaded == model {
			if loaded == nil || n.Latency < loaded.Latency {
				loaded = n
			}
		}
		if best == nil || n.RAMFree > best.RAMFree {
			best = n
		}
	}
	if loaded != nil {
		return loaded
	}
	return best
}

// ShardHolders returns nodes that hold the given shard key
func (r *Registry) ShardHolders(shardKey string) []*NodeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*NodeInfo
	for _, n := range r.nodes {
		for _, s := range n.ShardIDs {
			if s == shardKey {
				out = append(out, n)
				break
			}
		}
	}
	return out
}

// All returns a snapshot of all registered nodes
func (r *Registry) All() []NodeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]NodeInfo, 0, len(r.nodes))
	for _, n := range r.nodes {
		out = append(out, *n)
	}
	return out
}

// --- HTTP handlers ---

func (r *Registry) HandleRegister(w http.ResponseWriter, req *http.Request) {
	var n NodeInfo
	if err := json.NewDecoder(req.Body).Decode(&n); err != nil {
		http.Error(w, "bad json", 400)
		return
	}
	r.Register(n)
	w.WriteHeader(200)
}

func (r *Registry) HandleHeartbeat(w http.ResponseWriter, req *http.Request) {
	var hb struct {
		ID      string `json:"id"`
		RAMFree int64  `json:"ram_free"`
	}
	if err := json.NewDecoder(req.Body).Decode(&hb); err != nil {
		http.Error(w, "bad json", 400)
		return
	}
	r.Heartbeat(hb.ID, hb.RAMFree)
	w.WriteHeader(200)
}

func (r *Registry) HandleNodes(w http.ResponseWriter, req *http.Request) {
	nodes := r.All()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodes)
}

func (r *Registry) HandleRoute(w http.ResponseWriter, req *http.Request) {
	model := req.URL.Query().Get("model")
	node := r.BestForModel(model)
	if node == nil {
		http.Error(w, "no available node", 503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(node)
}

// SweepLoop runs periodic eviction of stale nodes
func (r *Registry) SweepLoop() {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for range t.C {
		r.Sweep()
	}
}
