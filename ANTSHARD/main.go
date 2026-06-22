// ant/main.go — ANT node entrypoint
// TitanU · June 19, 2026 · JCH-2026

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"time"
)

var (
	flagPort     = flag.Int("port", 7700, "ANT node listen port")
	flagModel    = flag.String("model", "", "Path to GGUF model file")
	flagRAMLimit = flag.Int64("ram-limit", 0, "Max RAM to use in MB (0 = auto-detect)")
	flagPeer     = flag.String("peer", "", "Bootstrap peer address (host:port)")
	flagRole     = flag.String("role", "node", "Role: node | registry | shard")
	flagShardDir = flag.String("shard-dir", "", "Directory containing shard files (shard role)")
)

func main() {
	flag.Parse()

	nodeID := generateID()
	addr := fmt.Sprintf("0.0.0.0:%d", *flagPort)

	ramFree := detectRAM(*flagRAMLimit)
	log.Printf("[ant] starting node %s on %s (%.1f GB free)", nodeID, addr, float64(ramFree)/1e9)

	self := NodeInfo{
		ID:          nodeID,
		Addr:        fmt.Sprintf("localhost:%d", *flagPort),
		RAMFree:     ramFree,
		ModelLoaded: modelName(*flagModel),
		LastSeen:    time.Now(),
	}

	mesh := NewMesh(self)
	registry := NewRegistry()
	registry.Register(self)

	mux := http.NewServeMux()

	// Mesh gossip
	mux.HandleFunc("/ant/gossip", mesh.ServeGossip)

	// Registry
	mux.HandleFunc("/ant/register", registry.HandleRegister)
	mux.HandleFunc("/ant/heartbeat", registry.HandleHeartbeat)
	mux.HandleFunc("/ant/nodes", registry.HandleNodes)
	mux.HandleFunc("/ant/route", registry.HandleRoute)

	// Inference proxy
	inf := NewInferenceProxy(*flagModel, ramFree)
	mux.HandleFunc("/ant/infer", inf.HandleInfer)
	mux.HandleFunc("/ant/health", inf.HandleHealth)

	// Shard
	if *flagRole == "shard" && *flagShardDir != "" {
		sm := NewShardManager(*flagShardDir)
		mux.HandleFunc("/ant/shard/push", sm.HandlePush)
		mux.HandleFunc("/ant/shard/pull", sm.HandlePull)
		mux.HandleFunc("/ant/shard/list", sm.HandleList)
		log.Printf("[ant] shard mode: serving from %s", *flagShardDir)
	}

	// Status page
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		nodes := registry.All()
		fmt.Fprintf(w, "ANT Node %s\nPeers: %d\nModel: %s\nRAM: %.1f GB\n",
			nodeID, len(nodes), self.ModelLoaded, float64(ramFree)/1e9)
	})

	// Background tasks
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go mesh.GossipLoop(ctx)
	go mesh.ListenLAN(*flagPort)
	go registry.SweepLoop()
	go heartbeatLoop(nodeID, registry, ramFree)

	// Bootstrap peer
	if *flagPeer != "" {
		log.Printf("[ant] bootstrapping from peer %s", *flagPeer)
		go bootstrapPeer(mesh, *flagPeer)
	} else {
		// LAN discovery
		go mesh.DiscoverLAN(*flagPort)
	}

	log.Printf("[ant] node ready at http://%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("[ant] server error: %v", err)
	}
}

func generateID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return "ant-" + hex.EncodeToString(b)
}

func modelName(path string) string {
	if path == "" {
		return ""
	}
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}

func detectRAM(limitMB int64) int64 {
	if limitMB > 0 {
		return limitMB * 1024 * 1024
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	// conservative: report half of system RAM as free
	// In production, read /proc/meminfo or sysctl
	total := int64(ms.Sys)
	if total < 512*1024*1024 {
		total = 4 * 1024 * 1024 * 1024 // fallback 4GB
	}
	return total / 2
}

func heartbeatLoop(id string, r *Registry, ramFree int64) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for range t.C {
		r.Heartbeat(id, ramFree)
	}
}

func bootstrapPeer(m *Mesh, addr string) {
	peer := NodeInfo{ID: "bootstrap", Addr: addr}
	m.AddPeer(peer)
	log.Printf("[ant] added bootstrap peer %s", addr)
}

func init() {
	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ltime | log.Lshortfile)
}
