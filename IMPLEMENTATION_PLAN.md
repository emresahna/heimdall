# Implementation Plan — EPIC-001: Honest health endpoints + server HTTP hygiene

Status: Draft
Epic: [EPIC-001 — Honest health endpoints + server HTTP hygiene](../docs/BACKLOG.md#epic-001--honest-health-endpoints--server-http-hygiene)
Prompt: `docs/ai/prompts/03-implementation-planning.md`
Priority: P0 · Phase 0 (Hygiene) · Effort: S
Scope: plan only — no code changes in this document.

---

## 1. Goal

Make the platform's own health signal truthful and reduce log noise:

1. `/healthz` must reflect real DB health (liveness === ready for a single-node server), falling back to
   truthful behavior, not a hardcoded `200 ok`.
2. Remove dead code (`RegisterStandardHealth`).
3. Stop logging one line per `SendLogs` batch.
4. Add missing timeouts to the agent metrics `http.Server` to match the server's hygiene.

Reference evidence (verified in current tree):

- `internal/server/http.go:38-41` — `handleHealth` always writes `200 ok`.
- `internal/server/health.go:67-69` — `RegisterStandardHealth` dead code (no callers).
- `internal/server/grpc.go:25` — `log.Printf("Received batch of %d logs", ...)` per batch.
- `cmd/agent/main.go:78-82` — metrics server sets only `ReadHeaderTimeout`.
- `internal/storage/clickhouse.go:138-145` — `(*DB).IsHealthy()` exists (2s `Ping`), already wired into both
  gRPC health (`cmd/server/main.go:52`) and `/readyz`.
- `deploy/k8s/server-deployment.yaml:45-50` and `helm/templates/server-deploy.yaml:56-61` — liveness →
  `/healthz`, readiness → `/readyz` (already correct; no probe path change needed).

---

## 2. Architecture

No architectural change; this touches the HTTP hygiene and operational honesty layer only.

**Proposed state**

| Concern | Now | After |
|---|---|---|
| `/healthz` | `200 ok` always | Delegates to `HealthChecker` (`IsHealthy()`); `503` when unhealthy |
| `/readyz` | checker-based | Unchanged |
| `RegisterStandardHealth` | dead code | deleted |
| `SendLogs` batch log | per-batch `log.Printf` | removed (errors already logged) |
| Agent metrics server | `ReadHeaderTimeout` only | full timeout set |

`handleHealth` becomes a thin wrapper over the same `HealthChecker` used by `handleReadyz`,
giving liveness and readiness identical semantics (acceptable for single-node ClickHouse per the
epic's Trade-offs; revisit with HA).

When no checker is configured (`NewHttpServer(db)` with no checker arg), tenant behavior is
preserved: both endpoints remain `200` (matches `handleReadyz` precedent and current unit tests).

---

## 3. Affected Files

**Code**

- `internal/server/http.go`
  - `handleHealth` — mirror `handleReadyz` checker logic (503 → body `"unhealthy"`, else `200` `"ok"`).
  - Optionally share a small `writeHealth(w, status, body)` helper to avoid duplicate boilerplate.
- `internal/server/health.go`
  - Delete `RegisterStandardHealth` (dead code). Keep `HealthChecker`, `HealthServer`, `NewHealthServer`,
    `Check`, `Watch`, `RegisterHealthService`.
- `internal/server/grpc.go`
  - Remove `log.Printf("Received batch of %d logs", len(req.Entries))`; keep the error log at `grpc.go:49`
    and the `metrics.EventsReceivedTotal` counter.
- `cmd/agent/main.go` (metrics server)
  - Add `ReadTimeout`, `WriteTimeout`, `IdleTimeout` to the `http.Server` literal at `:78-82`.

**Tests**

- `internal/server/http_test.go`
  - Replace `TestHealthzAlwaysOK` (asserts the lie) with `TestHealthzHealthy` (`200`) and
    `TestHealthzUnhealthy` (`503`).
  - Add a nil-checker case asserting `200` when constructed without a checker (parity with `/readyz`).

**Deploy / config**

- `deploy/k8s/server-deployment.yaml`, `helm/templates/server-deploy.yaml` — no probe path changes
  required. (Confirm only; document the stricter liveness semantics.)

**Docs**

- `README.md` — extend `## Troubleshooting (No Data)` with a "server /healthz failing" note and document
  the now DB-aware liveness probe.

---

## 4. Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Liveness now depends on ClickHouse → k8s restarts a degraded server earlier | Intended (epic trade-off) | Document in README + Helm values comments; acceptable for single-node CH |
| Transient CH ping failure flips liveness to 503 during startup/downscale | Low | `IsHealthy` uses a 2s-timeout ping; k8s `failureThreshold` / `initialDelaySeconds` absorb startups |
| Removing batch log hides useful debugging signal | Low | Error path (`grpc.go:49`) and `EventsReceivedTotal` metric remain; note in README that debug logging via a future flag is the place for per-batch detail |

No schema, proto, kernel, or data-format impact. No rollback of stored data involved.

---

## 5. Milestones

**M1 — Health truthfulness (core, ~0.5d)**

- `handleHealth` delegates to checker; `503 unhealthy` when unhealthy.
- Update `http_test.go` (healthz healthy/unhealthy/nil-checker cases).
- Delete `RegisterStandardHealth`.
- `go test -race ./internal/server/...` green; `golangci-lint run` clean.

**M2 — Log + server hygiene (~0.25d)**

- Remove per-batch `SendLogs` log line.
- Add Read/Write/Idle timeouts to agent metrics server.
- `go build ./...` green for both binaries.

**M3 — Docs + verification (~0.25d)**

- README troubleshooting + liveness semantics note.
- Manual kind verification (`./local_test.sh`): stop ClickHouse, confirm `/healthz` → 503 on both raw
  manifest and Helm deployments; liveness probe restarts the pod on recovery test.

---

## 6. Validation Plan

**Automated (CI, local)**

1. `go test -race ./internal/server/...` — healthz/readyz healthy + unhealthy + nil-checker cases.
2. `golangci-lint run ./...` (matches `.github/workflows/ci.yml`).
3. `go vet ./...`, `go build ./...`.
4. `go test -race ./...` — confirm no regressions.

**Manual / e2e (kind)**

1. Deploy via `deploy/k8s` and via Helm; both `/healthz` and `/readyz` return `200`.
2. `kubectl exec` into ClickHouse (`clickhouse-client query 'SELECT 1'`) or otherwise take CH down;
   `curl localhost:8080/healthz` on the server → `503 unhealthy`.
3. Restore ClickHouse; `/healthz` returns `200` again without server restart.
4. `/readyz` behavior is identical to before (unchanged code path).

**Acceptance criteria traceability**

| Acceptance criterion (backlog) | Verification |
|---|---|
| `/healthz` returns 503 when ClickHouse ping fails | M1 unit tests + M3 kind step 2 |
| Probes updated to match | No path change needed; confirmed both manifests point liveness→/healthz |
| No per-batch log line | M2 — grep confirms removal; server logs show only errors |
| Dead code removed | M1 — `rg "RegisterStandardHealth"` returns nothing |

---

## 7. Rollback Plan

- **Code:** safe to revert all five file-level changes; behavior returns to previous
  (lying healthz + noisy logs). No data migration involved.
- **Deploy:** no manifest/Helm values changes are required for this epic, so re-deploy of a prior
  server image fully restores prior behavior.
- **Migration steps:** none (no DB/proto/kernel/schema changes).
- **Feature flag:** not required; changes are small and behavior-restoring on revert.

---

## 8. Acceptance Criteria

Final checklist (mirrors `docs/BACKLOG.md` EPIC-001):

- [ ] `/healthz` returns `503` when ClickHouse ping fails, `200` when healthy or when no checker configured.
- [ ] `/readyz` behavior unchanged.
- [ ] `RegisterStandardHealth` deleted; no references remain.
- [ ] No per-batch `SendLogs` log line (errors still logged; `EventsReceivedTotal` metric retained).
- [ ] Agent metrics `http.Server` sets `ReadTimeout`, `WriteTimeout`, `IdleTimeout`.
- [ ] `internal/server/http_test.go` covers healthy, unhealthy, and nil-checker healthz cases.
- [ ] `go test -race ./...` green; `golangci-lint run ./...` clean.
- [ ] README troubleshooting + liveness semantics updated.
- [ ] Manual kind verification passed (CH down/up flips `/healthz` 503↔200) for both manifest and Helm deploys.

---

## 9. Out of Scope / Follow-ups

- grpc-health-probe as an alternative probe strategy (deferred; epic Future Work).
- Structured logging / debug-level infrastructure (would be the place to reintroduce a gated per-batch log).
- HA-aware liveness semantics (revisit in Phase 4).