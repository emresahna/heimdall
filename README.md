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

If you want an env file for local overrides, start from:

```bash
cp .env.example .env
```

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

On macOS (no local `bpftool`/Linux kernel), regenerate inside a container:

```bash
make generate-ebpf-docker
```

Generate both:

```bash
make generate
```

Generate kernel header used by eBPF:

```bash
make generate-vmlinux
```

> The eBPF Go bindings (`tracker_bpf.go`) and compiled object (`tracker_bpf.o`)
> are checked in — they are architecture-independent (little-endian eBPF) and
> keep normal builds toolchain-free. `vmlinux.h` is generated *transiently* when
> regenerating the bindings (requires a Linux host with `bpftool` and a
> BTF-enabled kernel, e.g. `Dockerfile.builder`) and is gitignored.
>
> **Regeneration workflow:** edit `internal/bpf/tracker.c` → run
> `make generate-ebpf-docker` (macOS) or `make generate-ebpf` (Linux) → commit
> `tracker.c`, `tracker_bpf.go`, and `tracker_bpf.o` together. Regeneration is
> reproducible — verified byte-identical on Docker Desktop's BTF-enabled kernel.
> The normal Docker image build (`make docker-agent`) never regenerates; it
> ships the committed object, so builds are deterministic and toolchain-free.

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
- `CLICKHOUSE_MAX_EXECUTION_TIME` (default: `60s`)
- `CLICKHOUSE_ASYNC_INSERT` (default: `true`)
- `USE_TLS` (default: `false`)
- `TLS_CERT_FILE` / `TLS_KEY_FILE` for gRPC TLS
- `PORT` (gRPC, default: `50051`)
- `HTTP_PORT` (UI/API, default: `8080`)
- `METRICS_PORT` (agent metrics, default: `9090`)
- `SHUTDOWN_TIMEOUT` (default: `5s`)

### Agent

- `SERVER_ADDR` (default: `localhost:50051`)
- `NODE_NAME` (default: hostname)
- `BATCHER_BATCH_SIZE` (default: `200`)
- `BATCHER_FLUSH_INTERVAL` (default: `2s`)
- `BATCHER_MAX_QUEUE` (default: `1000`)
- `BATCHER_RETRY_BACKOFF` (default: `200ms`)
- `K8S_ENRICH` (default: `false`)
- `HTTP_SAMPLE_BYTES` (default: `1024`)
- `CORRELATOR_TTL` (default: `30s`)
- `DIAGNOSTICS_INTERVAL` (default: `15s`, set `0` to disable)
- `CB_THRESHOLD` (default: `5`)
- `CB_RESET_TIMEOUT` (default: `30s`)
- `USE_TLS` / `TLS_CA_FILE` for mTLS-style client configuration when enabled

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

## Limitations

- **Plaintext HTTP/1.x only.** Capture hooks syscall tracepoints; TLS traffic (OpenSSL, Go TLS, sidecars) and HTTP/2 are not detected.
- **Whole-host capture.** All processes on the node are traced; there is no cgroup/pod scoping yet.
- **At-most-once delivery.** Batches are retried (with backoff) and then dropped on prolonged outages; there is no local spool.
- **Best-effort correlation.** Requests and responses are paired by `(pid, fd)`; fd reuse can mispair entries.
- **No body data.** Only the request line (method/path) and response status line are stored; the `payload` column is reserved but unused.
- **Little-endian BPF object.** The committed eBPF object is `bpfel` (works on amd64 and arm64 nodes); big-endian architectures are unsupported.
- **No authentication.** gRPC and HTTP endpoints are unauthenticated and TLS is off by default.

## License

[MIT](LICENSE). The eBPF program (`internal/bpf/tracker.c`) keeps the kernel-side
`Dual MIT/GPL` license declaration required by the BPF verifier for GPL helpers.
