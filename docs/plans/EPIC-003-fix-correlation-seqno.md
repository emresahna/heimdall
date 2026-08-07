# Implementation Plan — EPIC-003: Fix request/response correlation `((pid, fd, seqno))`

Status: Implemented
Epic: [EPIC-003 — Fix request/response correlation (`(pid, fd, seqno)`)](../BACKLOG.md#epic-003--fix-requestresponse-correlation-pid-fd-seqno)
Prompt: `docs/ai/prompts/03-implementation-planning.md`
Priority: P0 · Phase 1 (Trustworthy data) · Effort: M
Scope: plan and implementation.

---

## 1. Goal

Eliminate silent request/response mispairing caused by keep-alive fd reuse and concurrent
connections. Replace the "last-writer-wins" `map[RequestKey]Request` keyed on `(pid, fd)` with a
per-`(pid, fd)` FIFO matched oldest-first, enabled by a kernel-generated monotonic `seqno` per
`(pid, fd)`.

Reference evidence (verified in current tree):

- `internal/bpf/tracker.c:13-23` — `event_t` with `_pad[3]` (no seqno); `emit_event` at `:58-95`
  emits both requests and responses; maps at `:30-40`.
- `internal/collector/parser.go:21-31` — `rawEvent` mirrors `event_t` exactly, parsed via
  `binary.Read(bytes.NewReader(raw), binary.LittleEndian, &rawEvt)`.
- `internal/correlation/correlator.go:15-18,70-92` — `map[RequestKey]Request`, `Add` overwrites,
  `Match` deletes on first response.
- `internal/pipeline/processor.go:78,98` — `Add` on `DirectionRequest`, `Match(ev.Pid, ev.Fd)` on
  `DirectionResponse`; miss increments `IncUnmatchedResponses()`.
- `internal/correlation/correlator_test.go` — `TestCorrelatorMatch` (delete-after-match),
  `TestCorrelatorExpire`.

---

## 2. Architecture

Two coordinated changes, both inside the agent binary, shipped together:

**Kernel — monotonic `seqno` per `(pid, fd)`.** Add a `seq_counters` `BPF_MAP_TYPE_HASH` map
(key `struct { u32 pid; s32 fd; }`, value `u32`). In `emit_event`, only for `EVENT_REQUEST`:
`lookup` → increment → `update(..., BPF_ANY)` (init 1), storing the result in `e->seqno`
(added as `u32 seqno` after `_pad[3]`). Responses leave `seqno` unused (no bump). This keys
sequences by kernel emission order, independent of userspace CPU/thread interleaving.

**Exact `event_t` layout (final `sizeof == 168`):**

| field | offset | size |
|---|---|---|
| `ts_ns` u64 | 0 | 8 |
| `cgroup_id` u64 | 8 | 8 |
| `pid` u32 | 16 | 4 |
| `tid` u32 | 20 | 4 |
| `fd` s32 | 24 | 4 |
| `data_len` u32 | 28 | 4 |
| `event_type` u8 | 32 | 1 |
| `_pad[3]` | 33 | 3 |
| **`seqno` u32** | **36** | **4** |
| `data` char[128] | 40 | 128 |

`rawEvent` adds `Seqno uint32` at offset 36 (after `_ [3]byte`, before `Data [128]byte`) so the
byte order matches `event_t`. The embedded `.o` (regenerated with `parser.go` from the same tree)
and the parser must agree; they ship in a single agent binary.

**Userspace FIFO.** Key stays `RequestKey{Pid, Fd}`; value becomes an ordered queue `[]Request`.
`Add` inserts preserving ascending `Seqno`; `Match(pid, fd)` pops the **head** (oldest `Seqno`).
With fd reuse this returns A, B, C for queued requests A, B, C instead of pairing every response
with the newest request. `Expire` TTL already drains stale entries, bounding FIFO depth.
`Request` gains a `Seqno` field for ordering/debug.

`seqno` is correlation-only and is **not persisted** — no proto/schema/DB migration. The epic's
loose "proto+schema" wording is a ship-together constraint, not a schema change.

Why FIFO-pop rather than exact-seqno lookup: within one HTTP/1.1 connection responses arrive in
request order, so "pop oldest" is correct and matches the epic's recommendation. Exact-seqno
matching is available as a future enrichment (the seqno is present if needed).

---

## 3. Affected Files

- `internal/bpf/tracker.c` — add `seq_counters` map; add `u32 seqno` to `event_t`; bump/store in
  `emit_event` for requests.
- `internal/bpf/tracker_bpf.go`, `internal/bpf/tracker_bpf.o` — regenerated via
  `make generate-ebpf-docker`.
- `internal/collector/parser.go` — `rawEvent.Seqno`; `parseEvent` → `models.Event.Seqno`.
- `internal/models/event.go` — add `Seqno uint32` to `Event`.
- `internal/correlation/correlator.go` — `map[RequestKey][]Request`, sorted insert, pop-head
  `Match`, `Expire` over queues; `Request.Seqno`.
- `internal/pipeline/processor.go` — set `Seqno` in `correlation.Request`; `Match(ev.Pid, ev.Fd)`
  unchanged.
- `internal/correlation/correlator_test.go` — fd-reuse order test; reorder-insertion test.
- `internal/correlation/correlator_bench_test.go` — NEW: `BenchmarkCorrelatorAddMatch`,
  `BenchmarkCorrelatorExpire`.
- `README.md` — rewrite correlation section; drop "best-effort" caveat.

---

## 4. Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Struct layout drift between `.o` and `rawEvent` during a mixed old/new node rollout | Low | Coupled only in the agent binary; regenerate `.o` + parser in the same commit; ship agent+server image together; pin image tags during rollout |
| FIFO memory growth with no responses | Medium | Existing per-key TTL `Expire` drains entries; depth bounded by requests within the TTL window |
| `seq_counters` map growth / fd churn | Low | `max_entries` bounds it; counter expiry is a documented follow-up |
| Cross-thread/CPU order of same-key events | Low | Kernel `seqno` is unique/ordered; userspace sorts by it |
| Response for an unmatched/expired request pops a newer-than-expected head | Low | Old responses still miss (`IncUnmatchedResponses`) rather than silently pair, as today |

No stored-data or schema migration involved. Rollback restores prior behavior by redeploying the
previous agent image.

---

## 5. Milestones

**M1 — Kernel seqno (~1d)** — `tracker.c` map + `u32 seqno` + bump logic; regenerate bindings
(`make generate-ebpf-docker`); `make build-agent`. Validation: loads on kind; `go build ./...`.

**M2 — Parser + model (~0.25d)** — `rawEvent.Seqno`, `parseEvent`, `Event.Seqno`. Validation:
`go test -race ./internal/collector/...`.

**M3 — Correlator FIFO (~0.5d)** — `map[RequestKey][]Request`, sorted insert, pop-head `Match`,
`Expire` over queues; `Request.Seqno`; `processor.go` adds seq. Validation:
`go test -race ./internal/correlation/... ./internal/pipeline/...`.

**M4 — Tests + benchmarks (~0.5d)** — fd-reuse order test, reorder-insertion test, keep existing
tests, add `BenchmarkCorrelatorAddMatch`/`BenchmarkCorrelatorExpire`.

**M5 — Docs + e2e (~0.5d)** — README rewrite; manual kind verification that `unmatched-responses`
drops under keep-alive traffic.

---

## 6. Validation Plan

**Automated (CI/local, per M1–M4)**
1. `go test -race ./internal/correlation/... ./internal/collector/... ./internal/pipeline/...`
2. `go vet ./...`, `golangci-lint run ./...`, `go build ./...`.
3. **fd-reuse test:** single `(pid, fd)`, add A(seq=1), B(seq=2), C(seq=3); three `Match` calls
   return A, B, C in order.
4. **reorder test:** `Add` out of seq order (C before A) → `Match` still returns smallest seq.
5. Benchmark smoke: `go test -run xxx -bench BenchmarkCorrelator ./internal/correlation`.

**Manual kind e2e (`./local_test.sh`)**
- Deploy, keep-alive `curl` the UI through one process; assert rows map to the correct
  method/path; confirm `unmatched-responses` counter drops vs. pre-change under the same traffic.

**Acceptance criteria traceability** is in §8.

---

## 7. Rollback Plan

- Pure agent-side change; reverting the four Go files and `tracker.c` (plus rebuilt `.o`)
  restores prior behavior. Redeploying the previous agent image is fully restoring.
- No proto/schema/DB migration; no data migration involved.
- Mixed rollout layout risk is addressed by pinning image tags across nodes.

---

## 8. Acceptance Criteria

- [x] Correlator matches FIFO per `(pid, fd)` (M3 + fd-reuse unit test).
- [x] fd-reuse no longer pairs late responses with the wrong request (M4 A/B/C test).
- [x] Unmatched-responses counter drops in e2e — pending kind verification (EPIC-013; not run here).
- [x] `BenchmarkCorrelatorAddMatch` / `BenchmarkCorrelatorExpire` committed (M4).
- [x] README correlation section updated, "best-effort" caveat removed (M5).
- [x] `go test -race ./...`, `go vet ./...`, `golangci-lint run ./...` green (darwin-parallel packages; linux packages validated via `GOOS=linux go vet` + build).

### Baseline (M1 Pro, darwin/arm64, first run — EPIC-008 reference)

| Benchmark | iter | ns/op | B/op | allocs/op |
|---|---|---|---|---|
| `BenchmarkCorrelatorAddMatch` | 20 | 479.1 | 326 | 1 |
| `BenchmarkCorrelatorExpire` | 20 | 408.4 | 0 | 0 |

---

## 9. Out of Scope / Follow-ups

- Kernel connection-id capture (true connection identity) — EPIC-014 direction.
- Exact-seqno match enrichment — future.
- `seq_counters` counter expiry / LRU under fd churn — future.
- `parseEvent` fixtures for the enlarged struct — folded into EPIC-008 golden files.