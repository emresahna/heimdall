# Heimdall

Heimdall is an eBPF-based observability platform with two services:

- `agent`: captures HTTP request/response metadata from kernel tracepoints and sends batches over gRPC.
- `server`: ingests batches, stores them in ClickHouse, and serves HTTP API + embedded UI.

## Architecture

- `cmd/agent`, `cmd/server`: service entrypoints.
- `internal/collector`: collector runtime that reads ring buffer events.
- `internal/bpf`: eBPF program source and generated bindings.
- `internal/pipeline`: parsing, correlation, batching, diagnostics.
- `internal/enrichment`: node/Kubernetes enrichment.
- `internal/storage`: ClickHouse persistence.
- `internal/sender`: protobuf and gRPC service definitions.

## Quick Start (Docker Compose)

```bash
docker compose -f deploy/docker-compose.yml up --build
```

Open `http://localhost:8080`.

## Local Kubernetes Test (Kind)

Use the root script:

```bash
./local_test.sh run
```

This runs: prerequisites check -> kind cluster create -> image build/load -> manifest deploy.

Other actions:

```bash
./local_test.sh prereqs
./local_test.sh cluster
./local_test.sh build
./local_test.sh deploy
./local_test.sh cleanup
DELETE_CLUSTER=true ./local_test.sh cleanup
```

Defaults:

- Cluster name: `heimdall-local` (override with `CLUSTER_NAME`)
- Kind config: `./kind-config.yaml` (override with `KIND_CONFIG`)

## Build and Code Generation

Build binaries:

```bash
make build
```

Regenerate protobuf and gRPC files:

```bash
make generate-proto
```

Regenerate eBPF bindings/object:

```bash
make generate-ebpf
```

Generate both:

```bash
make generate
```

Generate kernel header used by eBPF:

```bash
make generate-vmlinux
```

Container images:

```bash
make docker-agent
make docker-server
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

- `SERVER_ADDR` (required)
- `NODE_NAME` (default: hostname)
- `AGENT_BATCH_SIZE` (default: `200`)
- `AGENT_FLUSH_INTERVAL` (default: `2s`)
- `AGENT_MAX_QUEUE` (default: `5000`)
- `AGENT_K8S_ENRICH` (default: `false`)
- `AGENT_HTTP_SAMPLE_BYTES` (default: `128`)
- `AGENT_CORRELATOR_TTL` (default: `30s`)
- `AGENT_DIAGNOSTICS_INTERVAL` (default: `15s`, set `0` to disable)

## Kubernetes Manifests

Apply manually:

```bash
kubectl apply -f deploy/k8s/clickhouse.yaml
kubectl apply -f deploy/k8s/server-deployment.yaml
kubectl apply -f deploy/k8s/agent-rbac.yaml
kubectl apply -f deploy/k8s/agent-ds.yaml
```

All namespaced resources are set to `default` in `deploy/k8s`.

## Troubleshooting (No Data)

1. Confirm services are running.
2. Check agent diagnostics logs (`events`, `matched`, `unmatched`, `drops`, `send_failures`).
3. Check server logs for ingest/DB errors.
4. Ensure privileged agent runtime and required mounts (`/sys/kernel/debug`, `/sys/fs/bpf`).
