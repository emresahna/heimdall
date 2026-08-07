# Implementation Plan — EPIC-004: Resolve the `payload` lie (ADR-003)

Status: Implemented — redaction pattern engine deferred to EPIC-016; this epic delivers the
fail-closed hook and request-head sampling path.
Epic: [EPIC-004 — Resolve the `payload` lie (ADR-003)](../BACKLOG.md#epic-004--resolve-the-payload-lie-adr-003)
Prompt: `docs/ai/prompts/03-implementation-planning.md`
Priority: P0 · Effort: S–M
Scope: plan and implementation.

---

## 1. Goal

The `payload` proto field, `models.LogEntry.Payload`, and ClickHouse `payload` column exist and already
round-trip through `processor → proto → ClickHouse`, but nothing ever populates them — every stored row
has an empty string. Make the schema honest by implementing **opt-in request-head sampling that is
default OFF**, applying a **redaction gate before anything is stored** (ADR-003 hard requirement), and
keeping the column. Reject the "drop column" option.

ADR-003 requires redaction before any body data is stored. The actual redaction *patterns* are the
responsibility of EPIC-016 (security hardening). EPIC-004 therefore introduces the `redact.Redactor`
**hook plus fail-closed gate** and leaves the pattern implementation to EPIC-016. Nothing unredacted can
reach storage.

## 2. Architecture and release contract

```text
kernel emit_event (≤128B request head, tracker.c)
        |
   processor.HandleEvent (request)
        |  truncate ev.Data to sampleMax
        |  parse request line (parseRequestLine)
        |  payload = redactor.Redact(ev.Data)   <-- NEW: redacted-or-empty only
        v
   correlation.Request{ ..., Payload }           <-- NEW field, set at Add
        |
   response event -> correlator.Match() -> entry.Payload = req.Payload
        |
   LogEntry.Payload -> batcher -> gRPC proto field 4 (unchanged)
        |
   ClickHouse payload column (unchanged: INSERT + SELECT already include it)
```

Fail-closed contract:

- If `HTTP_SAMPLE_BYTES` is `0` (the default), no payload is sampled, no redaction runs, and `payload`
  remains empty — identical behavior to today.
- If `HTTP_SAMPLE_BYTES > 0` but no `redact.Redactor` is configured (EPIC-016 not yet wired), the
  processor must **not** store bytes. It logs a warning and keeps `payload` empty.
- Once EPIC-016 supplies a `redact.Redactor`, sampled bytes are redacted at request time and stored.

## 3. Design decisions

1. **Redaction stays in EPIC-016.** EPIC-004 defines the `redact.Redactor` interface and the
   fail-closed gate only. Pattern list, parsing, and matching live in EPIC-016. This lets the P0 epic
   proceed now and honors ADR-003's hard requirement.
2. **Payload = request head bytes** carried through `correlation.Request` at `Add`, copied to
   `LogEntry.Payload` at `Match`. Response bytes are not stored.
3. **`HTTP_SAMPLE_BYTES` default 1024 → 0 (OFF)** in `config/env.go`, `.env.example`, and README.
   When `>0`, it is the cap on stored bytes; the 128-byte kernel ringbuf cap (`tracker.c:9`) is the
   real limit, so document `len(payload) ≤ min(HTTP_SAMPLE_BYTES, 128)`.
4. **No schema/proto migration.** proto field 4, `models.LogEntry.Payload`, the ClickHouse
   `payload` column, gRPC mapping (`internal/server/grpc.go:34`, `internal/transport/grpc.go:39`), and
   `QueryLogs` already carry `payload`. Only agent-side population + gate are added here.

## 5. Affected Files

### New `internal/redact` package

- `redact.go` — `type Redactor interface { Redact([]byte) []byte }`. The interface contract and
  fail-closed semantics. The pattern-based implementation intentionally belongs to EPIC-016.
- `redact_test.go` — unit test with a stub implementing `Redactor` to prove the contract (redact →
  replacement bytes; call seen). No pattern engine in this epic.

### Config `internal/config/env.go`

- `<HTTP_SAMPLE_BYTES>` default changes from `"1024"` to `"0"` (field `HTTPSampleBytes`, line 30).
- No redaction patterns here (EPIC-016). No config field added in this epic.

### Correlation `internal/correlation/correlator.go`

- Add `Payload string` to `Request` (carried from request `HandleEvent` to the matched entry).

### Processor `internal/pipeline/processor.go`

- Add `redactor redact.Redactor` and `payloadCap int` to `Processor`; extend `NewProcessor` with both.
- Request branch: truncate to `payloadCap`, parse; if `payloadCap > 0 && redactor != nil`, set
  `req.Payload = redactor.Redact(ev.Data)`; otherwise leave empty. Log once if sampling on without a
  redactor.
- Response branch: set `entry.Payload = req.Payload` (empty by default → behavior unchanged).
- Do **not** redact at response time; redact at request time so only redacted bytes ever transit.

### Wiring `cmd/agent/main.go`

- Pass the (currently nil/absent) redactor; when EPIC-016 provides one, pass it here.
- Emit a startup warning when `HTTPSampleBytes > 0` but no redactor is configured, restating that no
  bytes are stored until a redactor is supplied.

### Docs

- `README.md` — update `HTTP_SAMPLE_BYTES` default to `0` and explain semantics + the 128-byte cap;
  update the "No body data" limitation note to reflect opt-in sampling gated on a redactor.
- `.env.example` — `HTTP_SAMPLE_BYTES=0` (comment says off-default).

## 6. Risks and decisions

| Risk | Likelihood | Mitigation |
|---|---|---|
| Storing unredacted body violates ADR-003 | High if sampling on without redactor | Fail-closed gate: no redactor → no stored bytes, regardless of `HTTP_SAMPLE_BYTES` |
| Correlator memory grows with payload bytes | Medium | Payload bounded to `≤ min(HTTP_SAMPLE_BYTES, 128)` and only when sampling is enabled |
| Env loader regression on new default | Low | Existing reflection loader preserves `default` semantics; update `env_test` |
| Sampling silently changes stored size when enabled | Low | Docs state the cap; CI validates default remains empty; tests assert boundary |
| Confusion about which epic owns redaction | Low | EPIC-004 = interface + gate; EPIC-016 = pattern engine; documented in this plan |

## 7. Milestones

**M1 — Redaction interface + correlation field (~0.25d)**
- Create `internal/redact` with `Redactor` interface + test.
- Add `Payload` to `correlation.Request`.

**M2 — Processor wiring + gate (~0.25d)**
- Thread `payloadCap` and `redactor` through `Processor` and `NewProcessor`.
- Redact at request time, copy at match, fail closed when redactor absent.

**M3 — Default flip + docs (~0.25d)**
- `HTTP_SAMPLE_BYTES` default → `0` in env config, `.env.example`, README.
- README payload semantics + limitation note.

**M4 — Tests (~0.5d)**
- Processor flow test: with a stub redactor and `HTTP_SAMPLE_BYTES=128`, matched entry carries
  redacted payload ≤ cap; without redactor, payload empty even when sampling on.
- Redaction interface test; env_test for the new default.

## 8. Validation Plan

**Automated (every PR)**
1. `golangci-lint run`, `go vet ./...`, `go test -race ./...`, `go mod tidy` clean.
2. Processor unit test: request→response with a stub redactor yields `entry.Payload` = redacted bytes,
   `len ≤ HTTP_SAMPLE_BYTES`.
3. Processor test fail-closed: sampling on, no redactor → `entry.Payload` is empty; warning logged.
4. `env_test` asserts default `HTTP_SAMPLE_BYTES == 0`.
5. Correlation carry test: payload persists through `Add` → `Match`.

**Manual kind e2e (`local_test.sh`)**
1. Default config: `payload` empty in ClickHouse (behavior unchanged).
2. Set `HTTP_SAMPLE_BYTES` > 0 **without** a redactor: confirm `payload` still empty (fail-closed) and
   the warning is logged. This is the honest state until EPIC-016 lands.
3. Instrumented path (with a dev-only stub redactor, not committed): redacted payload stored and capped.

## 9. Rollback Plan

- **Before/t after merge:** revert config default, revert processor/correlator changes, remove
  `internal/redact`. No schema, proto, or data migration involved.
- **Deployed:** existing `payload` column is empty and unaffected; no row is re-written. Default
  behavior is identical, so rollback is safe at any time. If a sampling-on deployment exists, set
  `HTTP_SAMPLE_BYTES=0` to restore current behavior immediately.

## 10. Acceptance Criteria (mirrors BACKLOG EPIC-004)

- [x] Sampling off (default, `HTTP_SAMPLE_BYTES=0`): rows and `payload` are unchanged (empty).
- [x] Sampling on with a redactor: `payload` contains redacted bytes, `len ≤ HTTP_SAMPLE_BYTES`.
- [x] Adrs: fail-closed — sampling on with no redactor does **not** store bytes (ADR-003 hard
  requirement; EPIC-016 will supply the pattern engine).
- [x] No unredacted request bytes transit the pipeline; redaction happens at request time.

## 11. Out of Scope / Follow-ups

- Redaction pattern implementation and parsing — EPIC-016 (security hardening).
- Dropping the `payload` column — explicitly rejected.
- Response/body bytes storage beyond the head sample — future.
- Query-time body search / tokenization — EPIC-018.
- Configurable retention / LowCardinality schema work — EPIC-017.