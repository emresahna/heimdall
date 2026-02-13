# Heimdall

Heimdall is a two-part observability platform built on eBPF.

## Architecture
- `agent`: runs privileged, captures HTTP request/response metadata via eBPF, correlates request/response pairs, enriches metadata, and ships batches to the server.
- `server`: ingests batches over gRPC, stores them in ClickHouse, and serves HTTP API + embedded UI.

### Internal package layout
- `internal/agent/correlation`: request/response correlation state.
- `internal/agent/enrichment`: node and Kubernetes metadata enrichment.
- `internal/agent/httpparse`: HTTP line parsing.
- `internal/agent/pipeline`: event processing, batching, diagnostics.
- `internal/agent/transport`: outbound gRPC sender.
- `internal/server`: gRPC ingest and HTTP/UI handlers.
- `internal/telemetry`: shared telemetry domain types.

## Quick Start (Docker Compose)
```bash
docker compose -f deploy/docker-compose.yml up --build
```

Open `http://localhost:8080`.

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
### Server
- `CLICKHOUSE_ADDR` (default: `127.0.0.1:9000`)
- `CLICKHOUSE_DB` (default: `default`)
- `CLICKHOUSE_USER` (default: `default`)
- `CLICKHOUSE_PASSWORD` (default: empty)
- `PORT` (gRPC, default: `50051`)
- `HTTP_PORT` (UI/API, default: `8080`)
- `HTTP_SHUTDOWN_TIMEOUT` (default: `5s`)

### Agent
- `SERVER_ADDR` (required, gRPC address of server)
- `NODE_NAME` (default: hostname)
- `AGENT_BATCH_SIZE` (default: `200`)
- `AGENT_FLUSH_INTERVAL` (default: `2s`)
- `AGENT_MAX_QUEUE` (default: `5000`)
- `AGENT_K8S_ENRICH` (default: `false`)
- `AGENT_HTTP_SAMPLE_BYTES` (default: `128`)
- `AGENT_CORRELATOR_TTL` (default: `30s`)
- `AGENT_DIAGNOSTICS_INTERVAL` (default: `15s`, set `0` to disable periodic diagnostics logs)

## Local Docker Data Expectations
Heimdall does not use a fake producer. Data appears only when real HTTP traffic is captured by the eBPF agent.

- On native Linux Docker hosts, agent capture usually works as expected for traffic visible in the host kernel namespace.
- On Docker Desktop (macOS/Windows), containers run in a Linux VM. The agent can observe workloads inside that VM, but cannot observe macOS/Windows host processes.
- If your local stack is idle, UI will stay empty until real HTTP traffic is generated.

### Generate real local traffic (inside Heimdall Docker network)
```bash
NET=$(docker inspect heimdall_server --format '{{range $k,$v := .NetworkSettings.Networks}}{{$k}}{{end}}')
docker run --rm --network "$NET" curlimages/curl:8.12.1 \
  sh -c 'for i in $(seq 1 200); do curl -s http://server:8080/healthz >/dev/null; sleep 0.1; done'
```

Then query API directly:
```bash
curl -s "http://localhost:8080/api/logs?limit=20" | jq .
```

## Troubleshooting No Data (Local)
1. Confirm all services are running:
```bash
docker compose -f deploy/docker-compose.yml ps
```
2. Check agent logs for diagnostics counters (`events`, `matched`, `unmatched`, `drops`, `send_failures`):
```bash
docker compose -f deploy/docker-compose.yml logs -f agent
```
3. Check server logs for ingest/database errors:
```bash
docker compose -f deploy/docker-compose.yml logs -f server
```
4. Validate agent is privileged and has required mounts (`/sys/kernel/debug`, `/sys/fs/bpf`).
5. Re-run the traffic generation command and verify `/api/logs` returns entries.

## Kubernetes
```bash
kubectl apply -f deploy/k8s/clickhouse.yaml
kubectl apply -f deploy/k8s/server-deployment.yaml
kubectl apply -f deploy/k8s/agent-rbac.yaml
kubectl apply -f deploy/k8s/agent-ds.yaml
```

The agent RBAC is bound to the `default` namespace by default. Update `deploy/k8s/agent-rbac.yaml` if you deploy in a different namespace.
