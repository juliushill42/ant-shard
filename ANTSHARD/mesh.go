// ant/mesh.go — ANT P2P Mesh: discovery, gossip, routing
// TitanU · June 19, 2026 · JCH-2026

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"sync"
	"time"
)

// NodeInfo describes a peer in the ANT mesh
type NodeInfo struct {
	ID         string    `json:"id"`
	Addr       string    `json:"addr"`       // host:port
	RAMFree    int64     `json:"ram_free"`   // bytes
	ModelLoaded string   `json:"model"`      // GGUF filename or ""
	ShardIDs   []string  `json:"shard_ids"`  // shard keys this node holds
	LastSeen   time.Time `json:"last_seen"`
	Latency    int64     `json:"latency_ms"`
}

// Mesh manages the local node's view of the ANT network
type Mesh struct {
	mu       sync.RWMutex
	self     NodeInfo
	peers    map[string]*NodeInfo // id -> NodeInfo
	gossipCh chan NodeInfo
}

func NewMesh(self NodeInfo) *Mesh {
	return &Mesh{
		self:     self,
		peers:    make(map[string]*NodeInfo),
		gossipCh: make(chan NodeInfo, 256),
	}
}

// AddPeer upserts a peer into the local routing table
func (m *Mesh) AddPeer(n NodeInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n.LastSeen = time.Now()
	m.peers[n.ID] = &n
}

// RemoveStale drops peers not seen in the last 30 seconds
func (m *Mesh) RemoveStale() {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().Add(-30 * time.Second)
	for id, p := range m.peers {
		if p.LastSeen.Before(cutoff) {
			log.Printf("[mesh] evicting stale peer %s (%s)", id, p.Addr)
			delete(m.peers, id)
		}
	}
}

// Route picks the best node for an inference job.
// Strategy: prefer nodes with model loaded + most free RAM, then lowest latency.
func (m *Mesh) Route(modelName string) *NodeInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var best *NodeInfo
	for _, p := range m.peers {
		if p.ModelLoaded != modelName {
			continue
		}
		if best == nil || p.RAMFree > best.RAMFree {
			best = p
		}
	}
	if best != nil {
		return best
	}
	// fallback: any node with enough RAM to load
	for _, p := range m.peers {
		if p.RAMFree > 2*1024*1024*1024 { // >2GB
			if best == nil || p.RAMFree > best.RAMFree {
				best = p
			}
		}
	}
	return best
}

// Snapshot returns a copy of the peer table for gossip or inspection
func (m *Mesh) Snapshot() []NodeInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]NodeInfo, 0, len(m.peers))
	for _, p := range m.peers {
		out = append(out, *p)
	}
	return out
}

// GossipLoop periodically broadcasts self info and pulls from a random peer
func (m *Mesh) GossipLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.RemoveStale()
			peers := m.Snapshot()
			if len(peers) == 0 {
				continue
			}
			target := peers[rand.Intn(len(peers))]
			go m.pushGossip(target.Addr)
		}
	}
}

func (m *Mesh) pushGossip(addr string) {
	m.mu.RLock()
	self := m.self
	m.mu.RUnlock()

	self.LastSeen = time.Now()
	body, _ := json.Marshal(self)
	url := fmt.Sprintf("http://%s/ant/gossip", addr)
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	_ = body
}

// ServeGossip handles incoming gossip from peers
func (m *Mesh) ServeGossip(w http.ResponseWriter, r *http.Request) {
	var n NodeInfo
	if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	m.AddPeer(n)
	// respond with our own peer table (pull-push gossip)
	peers := m.Snapshot()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(peers)
}

// DiscoverLAN broadcasts a UDP beacon to find peers on the local network
func (m *Mesh) DiscoverLAN(port int) {
	beacon := fmt.Sprintf(`{"ant":"beacon","id":"%s","addr":"%s"}`, m.self.ID, m.self.Addr)
	bcast := net.UDPAddr{IP: net.IPv4bcast, Port: port + 1}
	conn, err := net.DialUDP("udp", nil, &bcast)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.Write([]byte(beacon))
}

// ListenLAN listens for UDP beacons from peers
func (m *Mesh) ListenLAN(port int) {
	addr := &net.UDPAddr{Port: port + 1}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Printf("[mesh] LAN discovery listen error: %v", err)
		return
	}
	defer conn.Close()
	buf := make([]byte, 1024)
	for {
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		var msg map[string]string
		if err := json.Unmarshal(buf[:n], &msg); err != nil {
			continue
		}
		if msg["ant"] == "beacon" && msg["id"] != m.self.ID {
			log.Printf("[mesh] LAN beacon from %s at %s", msg["id"], remote.String())
			peer := NodeInfo{ID: msg["id"], Addr: msg["addr"]}
			m.AddPeer(peer)
		}
	}
}
