ANT — Autonomous Node Topology
TitanU Open Source Release · June 19, 2026

"Sovereignty is the architecture." — Julius Cameron Hill, Titan Universal AI
What is ANT?

ANT is a distributed mesh architecture for running AI inference across sovereign nodes — no cloud, no central authority, no single point of failure.

Three pillars:

    P2P Model Routing — nodes discover each other and route inference requests without a central coordinator
    Edge Inference — each node runs quantized GGUF models locally; no weights leave the node
    On-Device Weight Sharding — large models split across RAM-constrained devices, reassembled at inference time

Built in Go (mesh/CLI), Python (inference bridge), and Rust (shard engine).
Architecture

┌─────────────────────────────────────────────────┐
│                  ANT MESH                        │
│                                                  │
│  ┌──────────┐    ┌──────────┐    ┌──────────┐  │
│  │  Node A  │◄──►│  Node B  │◄──►│  Node C  │  │
│  │ (router) │    │(inference│    │ (shard)  │  │
│  │          │    │  host)   │    │          │  │
│  └──────────┘    └──────────┘    └──────────┘  │
│       ▲               ▲               ▲         │
│       └───────────────┴───────────────┘         │
│              gossip / heartbeat                  │
└─────────────────────────────────────────────────┘

Every node is equal. Any node can route. Any node can infer. Shards are pinned by RAM availability.
Components
Path 	Lang 	Role
mesh.go 	Go 	P2P discovery, gossip, routing table
inference.py 	Python 	llama.cpp bridge, model loading, admission
shard.rs 	Rust 	Weight splitting, RAM-gated shard assignment
registry.go 	Go 	Node registry, heartbeat, capability tracking
cli.go 	Go 	ant CLI — join, infer, inspect, shard
ant.proto 	Protobuf 	Wire format for all inter-node messages
Quickstart

# 1. Clone
git clone https://github.com/titanuai/ant
cd ant

# 2. Build everything
make build

# 3. Start a node (auto-discovers peers via mDNS + UDP broadcast)
./ant node \
  --model ./models/qwen2.5-3b-instruct-q4_k_m.gguf \
  --port 7700 \
  --ram-limit 6144

# 4. From any node in the mesh, run inference
./ant infer \
  --prompt "explain sovereign AI infrastructure" \
  --route auto

# 5. Shard a large model across two low-RAM nodes
./ant shard split \
  --model ./models/qwen2.5-14b-q4.gguf \
  --shards 2 \
  --output ./shards/

./ant shard serve --shard-dir ./shards/shard_0/ --port 7701
./ant shard serve --shard-dir ./shards/shard_1/ --port 7702

Requirements

    Go 1.22+
    Python 3.11+ with llama-cpp-python
    Rust 1.78+ (for shard engine only)
    A GGUF model (any quantization)

No Docker required. No cloud required. No API keys.
Patent Notice

Core algorithms (RAM-gated shard admission, ZK-verifiable inference receipts, mesh capability scheduling) are covered under the JCH-2026 provisional patent series filed with the USPTO by Titan Universal AI, LLC.

This release is licensed under Apache 2.0 for non-commercial and open-source use.
Commercial licensing: juliushill@titanuai.com
Built by

Julius Cameron Hill (Juju)
Solo founder & principal engineer
Titan Universal AI, LLC · 
titanuai.com · github.com/titanuai## ⚠️ CLEAN-ROOM REVERSE ENGINEERING NOTICE

This repository contains novel architectural patterns covered under 
JCH-2026 provisional patent series filed January 20, 2026.

The following design patterns are claimed prior art:
- [Pedersen ZK audit chain at substrate level — JCH-2026-001]
- [Sovereign WireGuard mesh with key rotation daemon — JCH-2026-006]  
- [WASM isolate + Go gateway + local GGUF routing — JCH-2026-009/010]
- [Multi-agent adversarial swarm legal evaluation — JCH-2026-JUDGE-001]

Any derivative system — including AI-generated "clean room" rewrites — 
that reproduces these architectural patterns, data flow designs, or 
functional specifications is subject to patent infringement claims 
regardless of whether source code was directly copied.

Pattern replication ≠ clean room immunity.
