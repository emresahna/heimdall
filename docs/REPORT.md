# Heimdall — Project Review

A honest, developer-level assessment of the eBPF observability platform, written for the solo maintainer. All file references are to the current tree.

---

## 1. What this is

- **Agent**: kernel-side eBPF programs hook `write`/`sendto`/`writev`/`read`/`recvfrom` syscall tracepoints, detect HTTP request/response prefixes, and stream 128-byte samples into a ring buffer (`internal/bpf/tracker.c`). Userspace correlates request→response by `(pid, fd)`, enriches with Kubernetes pod metadata, batches, and ships over gRPC.
- **Server**: gRPC ingest → ClickHouse (`MergeTree`, day partitions, 7-day TTL), plus an HTTP API and an embedded dependency-free web UI.
- **Ops story**: Docker Compose, raw k8s manifests, a Helm chart, and a `kind`-based local test script. 19 commits, one developer, no CI, no tags.

---

## 2. Overall verdict

**Genuinely impressive for a solo project.** The architecture is clean, the code is readable, the failure handling (retries, circuit breakers, diagnostics) shows engineering maturity beyond most hobby projects. The core idea — syscall-level HTTP capture with zero-instrumentation — is sound and the implementation is a solid first vertical slice.

The main problems are not *what* you built but *what you haven't built yet*: the capture is only correct for plaintext, non-HTTP/2 traffic; there's no endpoint (IP/port) information; the whole host is captured (no cgroup scoping); there is no CI, no licensing, and little test coverage. It's a strong prototype in need of a hardening pass and a decision about what the product actually is.

---

## 3. What's done well

### Architecture & code quality
- Clean `cmd/` + `internal/` layout with good package separation (`collector`, `pipeline`, `transport`, `storage`, `server`). Interfaces (`Sender`, `Enricher`, `HealthChecker`) are used where they matter (`internal/transport/grpc.go:12`).
- Config is reflection-driven with env aliases, defaults, and validation (`internal/config/env.go`) — no external dependency, no duplication of parsing logic.
- Generated code (protobuf, bpf2go) is properly separated from handwritten code, and generated artifacts are checked in so builds don't need toolchains (`internal/bpf/gen.go`).

### Resilience engineering
- Circuit breaker on both the agent→server path and the server→ClickHouse path (`internal/transport/circuit_breaker.go`, `internal/storage/clickhouse.go:128`), with Prometheus gauges for state transitions.
- Retry with exponential backoff on ClickHouse inserts (`internal/storage/clickhouse.go:147`).
- Diagnostics counters with delta reporting (`internal/pipeline/diagnostics.go`) — this is a great "what do I tell the user when there's no data" feature, and the README troubleshooting section builds on it.
- Sharded correlation map to reduce lock contention (`internal/correlation/correlator.go`) — thoughtful concurrency work for a hot path.
- BPF resource hygiene: memlock removal, link cleanup, ring-buffer reader with proper close on shutdown (`internal/collector/collector.go`).

### Kernel-side engineering
- Cheap prefix filtering in-kernel before emitting events (`internal/bpf/tracker.c:42`), 128-byte sample cap, and the `pending_reads` tid-keyed map for read/recvfrom correlation are all correct patterns.
- Monotonic-clock → wall-clock conversion is done once with `sync.Once` (`internal/collector/parser.go:34`).

### Ops
- Multi-stage Dockerfiles with a `scratch` server image (`Dockerfile.server`), a working `local_test.sh` kind flow, and a real Helm chart.
- Conventional commit messages and a tidy README with configuration reference.

---

## 4. What's wrong — ranked by severity

### 4.1 Correctness / completeness of the data model (critical)

1. **TLS traffic is invisible.** All hooks are syscall tracepoints. Anything encrypted (OpenSSL, Go's TLS stack, sidecar meshes, HTTP/2 over TLS — i.e., most of a modern cluster) produces `GET /` plaintext through `write` only if the app writes plaintext. In practice TLS libraries write ciphertext through the same syscalls, and your prefix check (`internal/bpf/tracker.c:107`) simply discards it. The README and UI claim "HTTP telemetry" but the honest statement is "plaintext HTTP/1.x only." If you want to be a real observability platform, uprobes on OpenSSL/GnuTLS/BoringSSL's `SSL_write`/`SSL_read` (or the Go runtime's TLS write path) are the single biggest feature gap.

2. **HTTP/2 is not detected.** `PRI * HTTP/2.0` doesn't match any prefix check, and Go's HTTP/2 server-side data path doesn't emit HTTP/1.x status lines. Combined with #1, you're capturing a shrinking fraction of real traffic.

3. **No socket metadata — no IP, no port.** Events carry pid/fd/cgroup only. You cannot answer "which services talk to each other" or even which local port served a request. For an observability product this is the second-biggest gap (kprobe on `tcp_sendmsg`/`tcp_rcv` or `bpf_get_sock_ops` territory, or a `sock` map storing src/dst tuples).

4. **Whole-host capture, no scoping.** Tracepoints fire for every process on the host — kubelet, kube-proxy, the agent itself, other tenants' pods. You record `cgroup_id` but never filter on it. In a shared cluster this is noise and a privacy problem; in your own cluster it's wasted CPU. Filter to watched cgroups (or at least add a configurable allow/deny list) both in-kernel and in userspace.

5. **`payload` is a lie.** The ClickHouse schema has a `payload` column (`internal/storage/clickhouse.go:86`) and the proto carries it, but nothing ever sets it — `HandleEvent` drops the sampled bytes after parsing the request line (`internal/pipeline/processor.go:43-67`). Every stored row has an empty string. Either remove the column or implement opt-in body sampling.

### 4.2 Engineering bugs / sharp edges (high)

6. **fd-reuse correlation errors.** Request→response correlation keys on `(pid, fd)` (`internal/correlation/correlator.go:15`). With keep-alive connections being closed and fds recycled, or a thread pool serving concurrent connections, a late response can pair with a *different* request on the same fd. HTTP/1.1 sequential behavior masks it most of the time, but it's an incorrectness the platform can't detect. Correlate by `(pid, fd, seqno)` or capture a kernel-level connection id instead.

7. **Blocking I/O in the ring-buffer hot path.** `HandleEvent` runs synchronously inside the single ring-buffer read loop (`internal/collector/collector.go:96-111`), and `K8sEnricher.Enrich` reads `/proc/<pid>/cgroup` from disk on cache miss (`internal/enrichment/enricher.go:279`). At high event rates this stalls ring-buffer consumption → kernel drops → data loss. Offload to a worker pool; never do file I/O in the consumer.

8. **Per-event allocations in the parser.** `binary.Read(bytes.NewReader(raw), ...)` allocates a reader + boxing per event (`internal/collector/parser.go:48`). For an agent whose entire job is throughput, parse via direct offset reads from the byte slice.

9. **Synchronous server ingest with long blocking.** `SendLogs` calls `InsertBatch` inline; retry sleep can hold the gRPC handler for ~6s (`internal/storage/clickhouse.go:164-174`), while the agent's send timeout is 3s (`internal/transport/grpc.go:52`). Under ClickHouse pressure you get a thundering herd of expiring client requests piling into the server. Use a bounded ingest queue + worker pool (or rely on async inserts with a dedicated insert loop).

10. **Batch-at-most-once = silent data loss.** On retry exhaustion the agent drops the batch (`internal/pipeline/batcher.go:63-66`). Acceptable, but with no local spool there is zero durability during outages; document this as at-most-once and consider a disk-backed queue later.

11. **No server-side limits.** Unauthenticated gRPC accepts arbitrary batch sizes (`internal/sender/log.proto`) with no max-message / flow control. A bad agent can OOM the server.

12. **`writev` only reads the first iovec** (`internal/bpf/tracker.c:150`). Headers frequently span iovecs (Go's `net/http` splits header/body). Also missing: `readv`, `recvmsg`, `sendfile`. Consider `bpf_probe_read_user_str` loops or `process_vm_readv`-style helpers only as a last resort — real fix is hooking at a higher level (see #1/#3).

### 4.3 Security (high, because it's an observability platform)

13. **No auth anywhere.** gRPC and HTTP are wide open; TLS exists but defaults off (`internal/config/env.go:63`). Any process in the cluster can query all captured requests (paths, pods, namespaces) or inject fake data.
14. **`privileged: true`** for the agent (`deploy/k8s/agent-ds.yaml:25`) with no seccomp/AppArmor, no capability narrowing. For a home setup it's fine; for anything shared, restrict to `CAP_BPF|CAP_SYS_ADMIN` + `CAP_PERFMON` + seccomp-unconfined and note that `rlimit.RemoveMemlock` is a no-op on modern kernels.
15. **Hardcoded ClickHouse credentials** committed in manifests and Helm defaults (`helm/values.yaml:36`, `deploy/k8s/server-deployment.yaml:30`). Fine for local, but wire in a Secret soon.
16. **No CSP/security headers** on the HTTP server, no `ReadTimeout`/`WriteTimeout`/`ReadHeaderTimeout` on `http.Server` (`cmd/server/main.go:53`).

### 4.4 Operations / product gaps (medium)

17. **`/healthz` lies.** Both liveness and readiness probes hit `/healthz`, which always returns `ok` (`internal/server/http.go:32`), even though you built a DB-aware gRPC health service (`internal/server/health.go`). The last commit message claims "database-aware health checks" — wire the probe to reality.
18. **Log spam per batch**: `log.Printf("Received batch of %d logs")` on every ingest (`internal/server/grpc.go:25`).
19. **UI stats are computed over 200 rows client-side** (`web/app.js:134`). The "P95" and "Error Rate" cards are misleading — they're percentiles of the current page, not the window. Move aggregation to the server (ClickHouse `quantile`, `countIf`), or at least rename them.
20. **No pagination in the UI** (hard-coded `limit=200`, `web/app.js:74`).
21. **Metrics are exposed but unscrapable** in the shipped configs: no ServiceMonitor/annotations, no scrape config, no dashboards. Add a Grafana dashboard for the `heimdall_*` metrics — it sells the platform better than the console does today.
22. **ClickHouse schema could be much cheaper**: `method`/`status`/`type` as `String`, no `LowCardinality`, no `ORDER BY` on filter-heavy columns (pod/namespace), and `path LIKE '%x%'` can't use indexes (`internal/storage/clickhouse.go:216`). Add secondary skip indexes once data grows; TTL (7 days) is fixed, not configurable.
23. **No release discipline**: no tags, no version in the chart/images (`tag: latest` everywhere), no CHANGELOG, no LICENSE file.

### 4.5 Engineering process (medium)

24. **No CI.** `go vet`/`go test` pass, but nothing enforces it; `.golangci.yml` (v2, `default: all`) exists and nothing runs it. One GitHub Actions workflow would catch most regressions.
25. **Tests cover ~30% of the code** (parser, correlator, config, circuit breaker; nothing for storage, server, enrichment, batcher, transport). No e2e test on top of the `kind` script you already have — that's a cheap, huge win.
26. **No benchmarks** on the hot paths (parser, correlator, batcher) — for an observability agent these are the most useful tests to have.
27. **Minor inconsistencies**: Makefile builds the agent with `CGO_ENABLED=1` but the Dockerfile with `CGO_ENABLED=0` (`Makefile:21`, `Dockerfile.agent:10`); `Dockerfile.builder` duplicates toolchain setup; `RegisterStandardHealth` (`internal/server/health.go:67`) is dead code; `vmlinux.h` (3.1 MB) is checked in (fine, but make the regen story explicit in README); BPF object is generated for amd64 only (`Makefile:43`) — add an arm64 build check or document it.

---

## 5. What's done right (worth doubling down on)

- **Diagnostics counters with deltas** (`internal/pipeline/diagnostics.go`) — the single most useful feature for a support-ability of an eBPF agent. Keep this pattern; consider exposing it via the `/metrics` endpoint instead of only logs.
- **Circuit breaker with component labels** — instrumented, testable, honest. Keep.
- **`sync.Once` boot-time computation, sharded correlator, prefix-checks in-kernel** — all the right instincts. Keep the "fail cheap in-kernel" principle and extend it.
- **The kind local test script** (`local_test.sh`) — this is effectively your future e2e test harness.
- **Dependency-light UI and server image** — easy to deploy, easy to reason about. Don't add frameworks "for the resume"; add them when the console needs real data density.
- **Conventional commits + generated-code discipline.**

---

## 6. Missing topics / things you haven't considered yet

1. **What is this product?** Decide the one-sentence answer. Options: (a) lightweight zero-instrumentation HTTP tracer for dev/kind clusters, (b) full k8s network observability (needs TLS, HTTP/2, endpoints, cgroup scoping), (c) a trace/diagnostic companion to existing stacks (emit OTLP instead of your own schema). The roadmap below is shaped by that choice.
2. **Telemetry semantics**: trace IDs (`X-Request-ID` propagation is impossible from syscalls without uprobes — but you can emit your own request id and show req/resp pairing), request/response byte counts, TCP-level stats (RTT, retransmits) which syscalls *can* see via `tcp_*` kprobes.
3. **Alerting**: error-rate spikes per pod/namespace; with ClickHouse you already have the query engine — a small rules engine or just a Prometheus exporter (expose aggregated `heimdall_http_*` metrics server-side) gets you there cheaply.
4. **Retention/scale policy**: configurable TTL, partition pruning, disk sizing guidance, ClickHouse backup story.
5. **Distribution**: container registry, release pipeline, Helm chart publishing, signed images. LICENSE first.
6. **Security model**: agent auth (mTLS with short-lived certs), per-namespace access control on the API, data redaction (paths can contain tokens/secrets — you currently store them unredacted).
7. **Kernel compat matrix**: document which kernels/architectures are tested (BTF requirement for CO-RE), and add a startup check with a clear error message (currently `log.Fatal` with a raw loader error).
8. **SLOs**: what's the target event rate / overhead? Put a number on it — it's the best filter for every perf decision you'll make.

---

## 7. What to focus on as a solo developer (prioritized)

**Phase 0 — hygiene (days):**
- LICENSE, CI (lint + vet + test + build both binaries + helm lint), `.gitignore` for `bin/`, fix the CGO inconsistency, wire `/healthz` to DB health.
- Write the honest "Limitations" section in README (plaintext HTTP/1.x only, whole-host capture, at-most-once).

**Phase 1 — make the data trustworthy (weeks):**
- Remove/implement `payload`; add fd+sequence correlation; add cgroup scoping; move enrichment off the hot path (worker pool).
- Benchmarks + tests for parser/correlator/batcher, and an e2e smoke test script (kind → generate traffic → assert rows appear).

**Phase 2 — product decision (weeks):**
- Pick the direction from §6.1. If (a): polish what exists, add endpoint info cheaply (sockaddr from `sendto`/`recvfrom` args — you already hook those syscalls!), add per-route aggregates, ship a dashboard. If (b): start the TLS/uprobe work — it's a project in itself; budget months, not weeks.
- Server-side aggregation endpoints so the UI stops lying with client-side stats.

**Phase 3 — hardening (continuous):**
- mTLS default, secrets via Helm, securityContext narrowing, max batch size + backpressure, graceful degradation (agent keeps sampling when server is down, spools to disk).

---

## 8. Final word

The bones are good — clean Go, honest failure handling, a working local dev loop, and a demonstrable demo. The risks are all in the *completeness of the capture model*: the current syscall-tracepoint approach quietly misses the majority of real-world traffic (TLS, HTTP/2, Go TLS stacks), and there's no endpoint information to make the data genuinely useful for debugging. A solo developer should fix correctness and observability-of-the-platform before adding features, and should let the metrics + dashboard sell the product while the capture layer matures.

Keep the kernel-side code simple and legible — that's where the moat is, and it's also where the least-tested bugs live.
