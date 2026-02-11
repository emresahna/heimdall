# Heimdall
Heimdall is a two-part observability platform built on eBPF.

**Architecture**
- `agent`: runs as a privileged DaemonSet, captures HTTP request/response metadata via eBPF, correlates and enriches events, then ships batches to the brain.
- `brain`: ingests batches over gRPC, stores them in ClickHouse, and exposes a minimal HTTP API + UI for search and exploration.

## Quick Start (docker-compose)
```bash
docker compose -f deploy/docker-compose.yml up --build
```

Then open `http://localhost:8080` for the UI.

## Build
```bash
bpftool btf dump file /sys/kernel/btf/vmlinux format c > internal/collector/vmlinux.h
go generate ./internal/collector/...
go build -o bin/agent ./cmd/agent
go build -o bin/server ./cmd/server
```

## Proto Regeneration
```bash
protoc --go_out=. --go_opt=paths=source_relative \
  --go-grpc_out=. --go-grpc_opt=paths=source_relative \
  internal/sender/log.proto
```

## Configuration
**Brain**
- `CLICKHOUSE_ADDR` (default: `127.0.0.1:9000`)
- `CLICKHOUSE_DB` (default: `default`)
- `CLICKHOUSE_USER` (default: `default`)
- `CLICKHOUSE_PASSWORD` (default: empty)
- `PORT` (gRPC, default: `50051`)
- `HTTP_PORT` (UI/API, default: `8080`)
- `DEV_FAKE_DATA` (default: `false`)
- `DEV_FAKE_INTERVAL` (default: `2s`)
- `DEV_FAKE_BATCH` (default: `25`)

**Agent**
- `SERVER_ADDR` (required, gRPC address of brain)
- `NODE_NAME` (default: hostname)
- `AGENT_BATCH_SIZE` (default: `200`)
- `AGENT_FLUSH_INTERVAL` (default: `2s`)
- `AGENT_MAX_QUEUE` (default: `5000`)
- `AGENT_K8S_ENRICH` (default: `false`)
- `AGENT_HTTP_SAMPLE_BYTES` (default: `128`)
- `AGENT_CORRELATOR_TTL` (default: `30s`)

## Kubernetes
```bash
kubectl apply -f deploy/k8s/clickhouse.yaml
kubectl apply -f deploy/k8s/server-deployment.yaml
kubectl apply -f deploy/k8s/agent-rbac.yaml
kubectl apply -f deploy/k8s/agent-ds.yaml
```

The agent RBAC is bound to the `default` namespace by default. Update `deploy/k8s/agent-rbac.yaml` if you deploy in a different namespace.

## Observability Notes
See `docs/observability.md` for benefits, limitations, and recommendations.
