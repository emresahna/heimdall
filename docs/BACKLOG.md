# Heimdall — Engineering Backlog

### EPIC-001 — Honest health endpoints + server HTTP hygiene

- **Background:** k8s liveness/readiness both hit `/healthz` which always returns `ok`
  (`internal/server/http.go:38-41`); a DB-aware gRPC health service exists (`internal/server/health.go`)
  and `/readyz` works, but probes don't use them.
- **Current State:** `handleHealth` writes 200 unconditionally. `RegisterStandardHealth`
  (`internal/server/health.go:67`) is dead code. `SendLogs` logs every batch (`internal/server/grpc.go:25`).
- **Evidence:** `deploy/k8s/server-deployment.yaml:39-50` (probes), `helm/templates/server-deploy.yaml`
  (same pattern), `cmd/server/main.go:50-52`.
- **Problem:** outages are invisible to k8s; the platform's own signal is noise.
- **Options:** (a) wire `/healthz` → DB health and keep `/readyz`; (b) expose gRPC health via grpc-health-probe.
- **Recommendation:** (a) — `handleHealth` delegates to `HealthChecker`; delete `RegisterStandardHealth`;
  demote batch log to debug (or rate-limited). Also add `ReadTimeout/WriteTimeout/ReadHeaderTimeout` on
  the agent metrics server (`cmd/agent/main.go:78-82`).
- **Trade-offs:** liveness vs readiness semantics blur (acceptable for single-node ClickHouse; revisit with HA).
- **Dependencies:** none.
- **Risks:** k8s may restart a degraded server earlier than today — desired behavior.
- **Effort:** S.
- **Priority:** P0.
- **Acceptance Criteria:** `/healthz` returns 503 when ClickHouse ping fails; probes updated to match;
  no per-batch log line; dead code removed.
- **Testing:** unit test for health handler with fake checker; manual kind verification.
- **Benchmarks:** none.
- **Documentation:** README troubleshooting + config reference updated.
- **Future Work:** grpc-health-probe as an alternative probe strategy.

### EPIC-002 — CI & release discipline

- **Background:** CI exists but no tags, no version stamping, `tag: latest` everywhere, no CHANGELOG,
  no helm lint job, no image publishing.
- **Current State:** `.github/workflows/ci.yml` lints/vets/tests/builds. Helm chart present
  (`helm/`), images `heimdall-agent:latest`/`heimdall-server:latest`.
- **Evidence:** `helm/values.yaml:7-9,24-26`, `deploy/k8s/agent-ds.yaml:22`.
- **Problem:** cannot roll back, cannot identify running versions, chart can regress silently.
- **Options:** (a) full release pipeline (registry + signed images); (b) minimal: version stamping +
  helm lint in CI + `helm package` on tag.
- **Recommendation:** (b) first. `-ldflags -X main.version` (+ expose in `/healthz` response),
  chart version bump + `helm lint` in CI, CHANGELOG from conventional commits, git tag → build+push
  workflow (registry configurable).
- **Trade-offs:** (a) adds registry/signing work now; defer.
- **Dependencies:** none.
- **Risks:** image tag drift between chart and binary version; mitigate by deriving image tag from git tag.
- **Effort:** M.
- **Priority:** P1.
- **Acceptance Criteria:** binaries report version; CI runs helm lint; tagged releases produce versioned
  images and charts; CHANGELOG present.
- **Testing:** CI green on a tag; `helm template` diff clean.
- **Benchmarks:** none.
- **Documentation:** release process section in README.
- **Future Work:** registry publishing, cosign signing, helm repo hosting (ADR-001 (c) gated).

### EPIC-003 — Fix request/response correlation (`(pid, fd, seqno)`)

- **Background:** keep-alive fd reuse and concurrent connections can mispair requests and responses.
- **Current State:** `RequestKey{Pid, Fd}` sharded map, TTL expiry 30s
  (`internal/correlation/correlator.go:15,44-62`), README documents "best-effort correlation".
- **Evidence:** `internal/correlation/correlator.go:79-91` (Match deletes key on first response).
- **Problem:** silent incorrectness — a late response pairs with the wrong request; no detection mechanism.
- **Options:** (a) userspace seqno per (pid, fd) at parse time; (b) in-kernel seqno (ADR-002);
  (c) kernel connection-id.
- **Recommendation:** (b) — in-kernel per-`(pid, fd)` counter incremented in `emit_event` for
  `EVENT_REQUEST`, carried in `event_t` (reuse `_pad[3]` or add `u32 seqno`); userspace stores a
  FIFO per key and matches oldest. Note: `_pad` size change alters struct layout — regenerate
  bindings + bump ringbuf struct handling (`internal/collector/parser.go:21-31`).
- **Trade-offs:** (c) is more correct (true connection identity) but far more kernel work; (b) fixes
  the observable mispairing at low cost.
- **Dependencies:** proto + schema + `tracker.c` + parser changes (same release).
- **Risks:** struct layout mismatch between old/new agent objects on a node — ship agent+server together.
- **Effort:** M.
- **Priority:** P0.
- **Acceptance Criteria:** correlator matches FIFO per (pid, fd); test proving fd-reuse no longer
  mispairs (same fd, sequential requests, late responses); unmatched-responses counter drops in e2e.
- **Testing:** extended correlator tests; property-style test with fd reuse; e2e via EPIC-013.
- **Benchmarks:** correlator Add/Match/Expire benchmarks (EPIC-008 baseline).
- **Documentation:** README correlation section updated (remove "best-effort" caveat).
- **Future Work:** kernel connection-id capture (EPIC-014 direction).

### EPIC-004 — Resolve the `payload` lie

- **Background:** `payload` column and proto field exist; nothing ever writes them.
- **Current State:** sampled bytes parsed then discarded (`internal/pipeline/processor.go:43-67`);
  empty `payload` stored for every row.
- **Evidence:** `internal/storage/clickhouse.go:86`, `internal/sender/log.proto:37`, `internal/models/log.go:15`.
- **Problem:** schema promises data it never delivers; 128-byte ringbuf copies run for nothing.
- **Options:** (a) drop column/field; (b) opt-in body sampling (default off) with redaction.
- **Recommendation:** (b) per ADR-003 — add `HTTP_SAMPLE_BYTES` already exists as config
  (`internal/config/env.go:30`) but is applied only to truncation, not storage; wire sampled data
  through `processor → batcher → proto → clickhouse` when enabled; apply redaction (EPIC-016) before store.
- **Trade-offs:** (b) grows rows; mitigated by default-off and cap.
- **Dependencies:** EPIC-016 (redaction) for body data path.
- **Effort:** S–M.
- **Priority:** P0.
- **Acceptance Criteria:** with sampling off (default), rows are unchanged; with sampling on, `payload`
  contains ≤ `HTTP_SAMPLE_BYTES` bytes; redaction strips configured patterns.
- **Testing:** unit tests for processor payload plumbing; storage round-trip test.
- **Benchmarks:** parser benchmark with sampling enabled vs disabled.
- **Documentation:** config reference; README limitations section updated.
- **Future Work:** query-time body search (needs tokenization — EPIC-018).

### EPIC-005 — Cgroup scoping

- **Background:** whole-host capture; `cgroup_id` recorded but never filtered.
- **Current State:** every process on the node traced (`internal/bpf/tracker.c:80`); kubelet,
  kube-proxy, other tenants, the agent itself all captured.
- **Evidence:** `internal/bpf/tracker.c:79-84`, README limitations.
- **Problem:** privacy leak in shared clusters; wasted CPU; noisy UI.
- **Options:** (a) in-kernel allowlist hash map of cgroup ids checked before `emit_event`;
  (b) userspace-only filter (still burns kernel cycles); (c) cgroup v2 subtree filter via
  `BPF_CGROUP_*`-style attach.
- **Recommendation:** (a) — configurable comma-separated cgroup ids / prefix list
  (`CGROUP_FILTER`, default empty = capture all, preserving current behavior); map populated at
  startup and reloadable via a small control loop; check `bpf_map_lookup_elem` in `emit_event`.
- **Trade-offs:** prefix matching adds complexity; start with exact ids, add prefix later.
- **Dependencies:** none (agent-only).
- **Effort:** M.
- **Priority:** P0.
- **Acceptance Criteria:** with filter set, events outside the allowlist never reach ringbuf
  (verified via diagnostics + a metrics counter for filtered events); agent self-capture excluded by default.
- **Testing:** kernel-side logic is hard to unit test — cover via e2e (EPIC-013) + manual kind check;
  userspace config parsing unit tests.
- **Benchmarks:** measure events/s with filter set (expected improvement from less work).
- **Documentation:** README config + limitations.
- **Future Work:** per-namespace allow/deny via k8s API watch.

### EPIC-006 — Offload the hot path (worker pool + allocation-free parsing)

- **Background:** `HandleEvent` runs synchronously inside the single ringbuf loop; enrichment reads
  `/proc/<pid>/cgroup` on cache miss; parser allocates per event.
- **Current State:** `Collector.Run` calls `handler(event)` inline (`internal/collector/collector.go:96-112`);
  `binary.Read(bytes.NewReader(raw), …)` per event (`internal/collector/parser.go:48`);
  `containerIDFromPID` does disk I/O (`internal/enrichment/enricher.go:281-294`).
- **Problem:** at high event rates, slow handler → ringbuf backpressure → kernel drops → silent loss.
- **Options:** (a) worker pool with bounded queue between collector and processor; (b) async enrichment
  merged into worker step; (c) both.
- **Recommendation:** (c) — N workers (configurable, default = `GOMAXPROCS`); parser rewritten to
  direct offset reads from the byte slice (no `bytes.NewReader`/`binary.Read`); enrichment runs in the
  worker; keep event ordering lossy-but-bounded (queue drop counter already exists via
  `QueueDropsTotal`). Preserve `pending_reads` tid-keyed flow.
- **Trade-offs:** ordering is no longer guaranteed end-to-end (requests/responses may cross);
  acceptable because correlation is key-based, not order-based.
- **Dependencies:** none.
- **Risks:** unbounded goroutine growth — bound by queue + worker count; backpressure to BPF map remains
  (drop counter, `internal/collector/bpf_metrics.go`).
- **Effort:** M.
- **Priority:** P0.
- **Acceptance Criteria:** parser benchmark shows ≥2x reduction in allocs/op; ringbuf read loop never
  blocks on enrichment; queue drops visible in diagnostics and `/metrics`.
- **Testing:** parser fuzz/table tests against raw binary fixtures; collector smoke test with fake ringbuf.
- **Benchmarks:** parser (allocs/op, ns/op) and processor pipeline throughput — new benchmarks, EPIC-008.
- **Documentation:** architecture diagram note (collector → workers → correlator/enricher → batcher).
- **Future Work:** auto-tune worker count from drop counters.

### EPIC-007 — Server ingest queue + bounded backpressure

- **Background:** synchronous gRPC ingest with long blocking; retry sleeps can hold the handler ~6s
  while the agent times out at 3s.
- **Current State:** `SendLogs` → `InsertBatch` inline (`internal/server/grpc.go:48-52`);
  `insertWithRetry` sleeps 100ms/1s/5s (`internal/storage/clickhouse.go:164-174`); agent drops on
  exhaustion (`internal/pipeline/batcher.go:63-66`).
- **Problem:** ClickHouse pressure → thundering herd of expiring client calls; no server-side limits;
  gRPC accepts arbitrary batch sizes (default 4MiB max message only).
- **Options:** (a) bounded ingest channel + worker pool with async inserts; (b) rely on ClickHouse
  async inserts with a dedicated insert loop; (c) both.
- **Recommendation:** (c) — `SendLogs` enqueues (non-blocking with drop+metric when full);
  N insert workers drain the queue through the circuit breaker; gRPC `MaxRecvMsgSize` + per-batch
  entry cap; return `ResourceExhausted` instead of hanging.
- **Trade-offs:** adds at-most-once loss point at the server queue (counted, observable) —
  acceptable; documents the semantics honestly.
- **Dependencies:** EPIC-008 for insert-loop tests.
- **Effort:** M.
- **Priority:** P0.
- **Acceptance Criteria:** gRPC handler never blocks on ClickHouse; queue-full drops counted and
  exported; oversized batches rejected with `ResourceExhausted`; no behavior change when queue drains fast.
- **Testing:** server ingest tests with failing/fake ClickHouse; queue saturation test.
- **Benchmarks:** insert worker throughput with async_insert on/off.
- **Documentation:** README delivery semantics (at-most-once stated precisely).
- **Future Work:** disk-backed spool (at-least-once) — Phase 4.

### EPIC-008 — Tests + benchmarks for untested code

- **Background:** storage, server, enrichment, batcher, transport, collector have no tests.
- **Current State:** 423 test lines total (config, correlator, parsers, http, circuit breaker).
- **Evidence:** `internal/storage/clickhouse.go`, `internal/server/grpc.go`,
  `internal/enrichment/enricher.go`, `internal/pipeline/batcher.go` — zero test files.
- **Problem:** the highest-risk code (kernel-facing, I/O, retries) is the least verified; no benchmarks
  anywhere, so every perf claim in this backlog is unverified.
- **Options:** (a) unit tests only; (b) unit tests + benchmarks; (c) + e2e (EPIC-013).
- **Recommendation:** (b) now, (c) next — table tests for batcher flush/retry/drop, enricher cache
  hit/miss, storage query building (use `clickhouse-go` mock or an in-process ClickHouse in CI),
  grpc handler mapping; benchmarks: parser, correlator, batcher, proto marshal.
- **Trade-offs:** full ClickHouse integration tests are heavy; use SQL-builder isolation + one
  containerized integration test in CI.
- **Dependencies:** none.
- **Effort:** M.
- **Priority:** P0.
- **Acceptance Criteria:** coverage ≥60% on storage/batcher/enricher/grpc; benchmarks committed with
  baseline numbers recorded in this file; `go test -race ./...` green.
- **Testing:** this epic IS the testing epic.
- **Benchmarks:** new: `BenchmarkParseEvent`, `BenchmarkCorrelatorAddMatch`, `BenchmarkBatcherFlush`,
  `BenchmarkProtoMarshal`.
- **Documentation:** baseline numbers appended to this file after first run.
- **Future Work:** golden-file fixtures for parser bytes.

### EPIC-009 — Kernel compat matrix + startup check

- **Background:** CO-RE needs BTF; loader failure is currently a raw `log.Fatal`.
- **Current State:** `collector.New` fails with `fmt.Errorf("load objects: %w", err)`
  (`internal/collector/collector.go:54-56`); no version checks.
- **Evidence:** README documents BTF requirement only for regeneration (`Makefile:41-47`).
- **Problem:** bad first-run UX on old kernels; no documentation of what works.
- **Options:** (a) pre-flight check (kernel version + BTF presence via `/sys/kernel/btf/vmlinux`)
  with actionable error; (b) + docs table.
- **Recommendation:** (a) + (b) — check BTF file exists (and kernel ≥ 5.8) before load; emit
  actionable message; add `docs/kernel-support.md` table (tested kernels/archs; note `bpfel` only).
- **Trade-offs:** none meaningful.
- **Effort:** S.
- **Priority:** P1.
- **Acceptance Criteria:** agent on non-BTF kernel fails with a clear message; docs table exists.
- **Testing:** unit test for the check helper (inject fs paths).
- **Benchmarks:** none.
- **Documentation:** `docs/kernel-support.md` + README troubleshooting.
- **Future Work:** CI matrix on multiple kernel versions (kind nodes).

### EPIC-010 — Endpoint metadata (src/dst IP:port)

- **Background:** no IP/port anywhere; the two most useful questions ("which services talk", "which
  local port served") are unanswerable.
- **Current State:** hooks already see `sockaddr` for `sendto`/`recvfrom` but never read it.
- **Evidence:** `internal/bpf/tracker.c:117-135` (`sendto` args[3]=dest_addr, args[4]=addrlen),
  `tracker.c:219-229` (`recvfrom` same).
- **Problem:** observability product without endpoints is a log file with timestamps.
- **Options:** (a) in-kernel sockaddr read (cheap, per docs/tracker-c-development.md §3);
  (b) userspace `/proc/net` lookup (racy, slow); (c) `bpf_get_sock_ops` (bigger).
- **Recommendation:** (a) — read `sockaddr_in`/`sockaddr_in6` in `sendto`/`recvfrom` entry probes
  (IPv4 + IPv6, bound reads); extend `event_t` with `src_ip:port, dst_ip:port` for write-side and
  response-side; userspace passes through; schema + proto + UI gain columns (local/remote endpoint).
- **Trade-offs:** struct growth (+~24 bytes/event) — fine given 128B payload budget; wildcard binds
  (0.0.0.0) need `getsockname`-style enrichment or accept generality loss — accept, document.
- **Dependencies:** EPIC-003 (struct change window) — same release to avoid layout drift.
- **Effort:** M.
- **Priority:** P1 (highest ROI per tracker-c-development.md).
- **Acceptance Criteria:** stored rows include src/dst ip:port; UI shows endpoints; IPv6 handled.
- **Testing:** kernel-side logic validated via e2e; userspace parsing unit tests with crafted events.
- **Benchmarks:** event parse benchmark with the larger struct.
- **Documentation:** README data-model section; UI column additions.
- **Future Work:** TCP stats (RTT, retransmits) via tcp kprobes (Phase 3).

### EPIC-011 — Server-side aggregation + honest UI stats

- **Background:** P95/error-rate are computed client-side over the current 200-row page
  (`web/app.js:134-158`), so they're percentiles of the page, not the window — misleading.
- **Current State:** `/api/logs` returns raw rows; UI aggregates; hard-coded `limit=200`
  (`web/app.js:74,180`); no pagination controls.
- **Evidence:** `internal/server/http.go:63-66` caps limit at 1000; `web/index.html` has no pager.
- **Problem:** wrong numbers presented as fact; no way to browse beyond 200 rows.
- **Options:** (a) `/api/stats` endpoint (ClickHouse `quantile(0.95)`, `countIf(status>=400)`,
  per-minute buckets) + pagination on `/api/logs`; (b) rename UI cards to "page stats".
- **Recommendation:** (a) — honest numbers beat honest labels.
- **Trade-offs:** more API surface; keep `/api/logs` shape stable for compat.
- **Effort:** M.
- **Priority:** P1.
- **Acceptance Criteria:** P95/error-rate come from server aggregation over the selected window;
  UI paginates; chart uses server bucketed series (or documented as page-bucket approximation).
- **Testing:** aggregation query tests; http handler tests for `/api/stats`.
- **Benchmarks:** aggregation query latency on reference dataset.
- **Documentation:** API reference section in README.
- **Future Work:** per-route aggregates, top-N endpoints view.

### EPIC-012 — Platform observability (diagnostics on /metrics + dashboard)

- **Background:** the diagnostics counter story is excellent but log-only; metrics exist but nothing
  scrapes them.
- **Current State:** `StartDiagnosticsReporter` logs deltas (`internal/pipeline/diagnostics.go:104-146`);
  `heimdall_*` metrics exported on agent 9090 and server 8080; no ServiceMonitor/annotations, no
  dashboards, no scrape configs anywhere.
- **Evidence:** `cmd/agent/main.go:76-93`, `helm/values.yaml`, `deploy/k8s/agent-ds.yaml` (no
  `prometheus.io/scrape` annotations).
- **Problem:** "is the agent healthy?" requires reading logs; the platform can't be monitored with
  the tools it's meant to replace.
- **Options:** (a) expose diagnostics as Prometheus counters (double-write alongside existing
  `heimdall_agent_*` — or migrate to them); (b) + scrape config + Grafana dashboard JSON in-repo.
- **Recommendation:** (b) — map diagnostics snapshot onto existing `heimdall_agent_*` counters
  (they already exist: events_read, batches_sent, send_failures, queue_drops); add
  `prometheus.io/scrape` annotations to manifests; ship `docs/dashboards/heimdall.json`.
- **Trade-offs:** migrating diagnostics to the existing counters avoids new metric names; keep
  log-deltas for debugging.
- **Effort:** S–M.
- **Priority:** P1.
- **Acceptance Criteria:** agent metrics include all diagnostics deltas as counters; kind deployment
  scraped by a Prometheus (in e2e); dashboard JSON renders.
- **Testing:** e2e asserts metrics endpoint contains counters.
- **Benchmarks:** none.
- **Documentation:** dashboards + scraping docs.
- **Future Work:** alerting rules (error-rate spikes per pod/namespace).

### EPIC-013 — E2E smoke test (kind → traffic → assert rows)

- **Background:** `local_test.sh` is a working harness with no assertions — the cheapest big testing win.
- **Current State:** script rolls out the stack (`local_test.sh:64-79`); nothing generates traffic or
  checks ClickHouse rows.
- **Problem:** every capture-model change (EPIC-003/005/010) ships unverified end-to-end.
- **Options:** (a) script extension asserting rows exist after generated HTTP traffic;
  (b) GitHub Actions kind job running it.
- **Recommendation:** (a) + (b) — extend `local_test.sh` with `smoke` action: deploy → run a pod that
  `curl`s the server UI → wait → query ClickHouse via the server API → assert ≥1 row and expected
  method/path; run nightly or on-demand in CI (kind-action; privileged container support).
- **Trade-offs:** kind in CI needs privileged mode (supported by kind-action) and is slow (~5-10 min);
  keep it a manual/nightly trigger.
- **Effort:** M.
- **Priority:** P1.
- **Acceptance Criteria:** `./local_test.sh smoke` passes locally and in CI on demand; failure
  messages name the missing layer (agent events / correlation / ingest / storage).
- **Testing:** this is the e2e epic.
- **Benchmarks:** none.
- **Documentation:** README local-test section.
- **Future Work:** HTTP/2 + TLS traffic fixtures (post EPIC-014).

### EPIC-014 — HTTP/2 detection + TLS visibility (uprobes) [ADR-001 gated]

- **Background:** `PRI * HTTP/2.0` matches no prefix check; TLS writes ciphertext to the same
  syscalls; both invisible (`docs/tracker-c-development.md` §1-2).
- **Current State:** prefix checks `internal/bpf/tracker.c:42-56`; whole traffic share missed.
- **Problem:** capturing a shrinking fraction of real traffic.
- **Options:** (a) HTTP/2 preface detection + `PRI` type tagging (cheap, partial);
  (b) uprobes on `SSL_write`/`SSL_read` (OpenSSL/GnuTLS/BoringSSL/Go TLS) — separate project;
  (c) both, phased.
- **Recommendation:** (a) first (days): detect `PRI * HTTP/2.0`, tag event type `EVENT_H2`, store as
  `type="h2"`; then (b) as its own multi-week effort: per-library probes, session/connection id
  tracking, pid→lib mapping, fallback when symbols missing.
- **Trade-offs:** uprobes add attach-time risk and per-lib maintenance; (a) alone still misses TLS.
- **Dependencies:** ADR-001 decision; EPIC-003 struct window.
- **Effort:** (a) S; (b) L.
- **Priority:** P2.
- **Acceptance Criteria (a):** HTTP/2 requests recorded with `type=h2`; e2e fixture with an h2 server.
- **Acceptance Criteria (b):** TLS HTTP/1.1 + h2 captured on OpenSSL (and Go runtime TLS) with
  request/response pairing; library detection logged.
- **Testing:** e2e fixtures; uprobe attach tests in kind.
- **Benchmarks:** overhead of uprobe-enabled agent vs baseline.
- **Documentation:** capture matrix update.
- **Future Work:** per-connection keys (HTTP/2 streams) for correlation.

### EPIC-015 — Syscall coverage (writev iovecs, readv, recvmsg, sendfile)

- **Background:** `writev` reads only the first iovec; Go's `net/http` splits header/body across
  iovecs; `readv`/`recvmsg`/`sendfile` unhandled.
- **Current State:** `internal/bpf/tracker.c:137-170`.
- **Problem:** missed real requests even within plaintext HTTP/1.x.
- **Options:** (a) bounded iovec iteration (≤8 entries) in-kernel; (b) higher-level hooks (EPIC-014);
  (c) both.
- **Recommendation:** (a) — iterate with a cap; skip `sendfile` (server-side static files, low ROI)
  or add cheap `sys_enter_sendfile` prefix check later.
- **Trade-offs:** walking iovecs risks partial headers; mitigate: scan each iovec, emit first match,
  stop at cap.
- **Dependencies:** none.
- **Effort:** S–M.
- **Priority:** P2.
- **Acceptance Criteria:** requests whose request line spans iovecs are captured (e2e with Go http server).
- **Testing:** e2e; kernel verification on kind.
- **Benchmarks:** events/s with multi-iovec writes.
- **Documentation:** capture matrix update.
- **Future Work:** recvmsg for responses.

### EPIC-016 — Security hardening

- **Background:** no auth, TLS off by default, privileged agent, hardcoded credentials, no HTTP
  hardening, unredacted paths.
- **Current State:** `USE_TLS` default false (`internal/config/env.go:63-68`); gRPC + HTTP open;
  `privileged: true` (`deploy/k8s/agent-ds.yaml:25`); credentials in
  `deploy/k8s/clickhouse.yaml:51`, `deploy/k8s/server-deployment.yaml:31`, `helm/values.yaml:36`;
  `http.Server` has timeouts (`cmd/server/main.go:54-61`) but no CSP/security headers; paths with
  tokens/secrets stored as-is.
- **Evidence:** README "No authentication" limitation.
- **Problem:** an observability platform with no access control is a data exfiltration endpoint.
- **Options:** (a) phased: secrets → securityContext → HTTP hardening → auth; (b) all at once.
- **Recommendation:** (a) phased, in order:
  1. Secrets via Helm `existingSecret` + k8s Secret (break the hardcoded defaults).
  2. Agent `securityContext`: drop `privileged`, use `capabilities: [BPF, PERFMON, SYS_ADMIN]` +
     seccomp (note `rlimit.RemoveMemlock` is a no-op on modern kernels — `collector.go:49`).
  3. HTTP: security headers middleware, `Content-Security-Policy`, `Referrer-Policy`.
  4. Auth: mTLS for gRPC (short-lived certs) + API token for HTTP; redaction config for paths/query
     params (needed by EPIC-004).
- **Trade-offs:** mTLS is the biggest lift; do 1-3 first, 4 last.
- **Dependencies:** none (redaction work feeds EPIC-004).
- **Effort:** (1-3) M; (4) L.
- **Priority:** P2 (P1 for steps 1-2 if deployed outside kind).
- **Acceptance Criteria:** no hardcoded credentials in deployed manifests; agent runs unprivileged on
  kind with eBPF working; security headers present; mTLS handshake tested between agent/server;
  redaction patterns applied before storage.
- **Testing:** e2e with auth enabled; header assertions; unit tests for redaction.
- **Benchmarks:** redaction cost at event rate.
- **Documentation:** security model doc + README.
- **Future Work:** per-namespace API access control, audit logging.

### EPIC-017 — ClickHouse schema optimization + configurable retention (ADR-004)

- **Background:** schema is functional but expensive at scale; TTL fixed at 7 days.
- **Current State:** `String` columns, `ORDER BY (timestamp, pid, fd)`, `TTL timestamp + INTERVAL 7 DAY`
  (`internal/storage/clickhouse.go:76-97`); `path LIKE '%x%'` full scans (`clickhouse.go:215-217`).
- **Problem:** filter-heavy queries (pod/namespace) can't use indexes; retention not configurable;
  no partition pruning guidance.
- **Options:** (a) incremental `ALTER TABLE` changes (LowCardinality, skip indexes, configurable TTL);
  (b) new table + migration.
- **Recommendation:** (a) — `LowCardinality(String)` for `method/type/namespace/pod/node/container`,
  `minmax`/`ngrambf` skip index on `path` after volume threshold, TTL via env
  (`CLICKHOUSE_TTL_DAYS`, default 7) applied as `ALTER TABLE ... MODIFY TTL`, document disk sizing.
- **Trade-offs:** `path` skip index has build cost; add after data volume justifies it.
- **Dependencies:** none (server-side only).
- **Effort:** M.
- **Priority:** P2.
- **Acceptance Criteria:** filter queries on pod/namespace use indexes (EXPLAIN check); TTL configurable
  at deploy time; `ALTER TABLE` migration safe on existing deployments (pattern at `clickhouse.go:102-117`).
- **Testing:** migration test against a scratch ClickHouse; EXPLAIN assertions in integration test.
- **Benchmarks:** query latency before/after on synthetic dataset.
- **Documentation:** retention + sizing guide.
- **Future Work:** partition pruning automation, ClickHouse backup story.

### EPIC-018 — At-least-once durability (disk spool)

- **Background:** batches are dropped on retry exhaustion; outage = silent data loss.
- **Current State:** `internal/pipeline/batcher.go:63-66` (at-most-once), README documents it.
- **Problem:** no durability during outages; e.g., 15-min ClickHouse outage loses the window.
- **Options:** (a) disk-backed queue (WAL-style spool) on agent; (b) rely on server-side queue
  (EPIC-007) + accept loss at agent; (c) both.
- **Recommendation:** (a) later — only after EPIC-006/007 stabilize; spool with size cap + oldest-drop,
  flush order preserved, bounded disk usage.
- **Trade-offs:** disk I/O in agent; ordering complexity; needs clean shutdown semantics.
- **Dependencies:** EPIC-006, EPIC-007.
- **Effort:** L.
- **Priority:** P2.
- **Acceptance Criteria:** with server down for N minutes, rows are recovered after restart; spool
  bounded by configurable bytes; no unbounded disk growth.
- **Testing:** integration test with server stop/start; spool rotation tests.
- **Benchmarks:** spool append/read throughput.
- **Documentation:** delivery semantics section rewritten (at-least-once).
- **Future Work:** dedupe keys (request id) for exactly-once-ish ingest.
