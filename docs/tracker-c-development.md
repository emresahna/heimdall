# tracker.c — Further Development Assessment

AI-generated review of `internal/bpf/tracker.c` for the solo maintainer, framed against `docs/REPORT.md`. This covers what the kernel-side tracer is, what's missing, what's unnecessary, what it could become, and what it should never be.

---

## What tracker.c is

The heart of what Heimdall collects — a C program that runs **inside the Linux kernel**, attached to 7 syscall tracepoints. It watches process syscalls to detect HTTP traffic without instrumenting the application.

Two maps:
- **`events`** (ringbuf) — outbound channel. Each `struct event_t` carries timestamp, `cgroup_id`, pid/tid, fd, `data_len`, event_type (request=1 / response=2), and the first **128 bytes** of the HTTP payload.
- **`pending_reads`** (hash) — internal bookkeeping: remembers each thread's `read`/`recvfrom` args at entry, matched by `tid` on exit.

Seven probes, two strategies:
- **Writes → requests:** `sys_enter_write`, `sys_enter_sendto`, `sys_enter_writev` check the first 4 bytes (`is_http_request`: "GET ", "POST", "PUT ", "DELE", "PATC", "HEAD", "OPTI") and emit `EVENT_REQUEST`.
- **Reads → responses:** `sys_enter_read`/`sys_exit_read`, `sys_enter_recvfrom`/`sys_exit_recvfrom` correlate via `pending_reads`, check `is_http_response` ("HTTP"), emit `EVENT_RESPONSE`.

Pipeline: **kernel-side C (raw traffic collector) → ringbuf → Go `Collector.Run()` → parse/correlate → persist.** The agent hosts this; the server never sees `tracker.c`.

---

## What's missing (real gaps, ranked)

### 1. TLS is invisible — the single biggest hole (REPORT §4.1.1)
Every probe reads the syscall buffer and matches plaintext prefixes. TLS stacks write *ciphertext*, so `is_http_request` silently discards encrypted traffic. Fix = uprobes on `SSL_write`/`SSL_read` (OpenSSL/GnuTLS/BoringSSL/Go TLS). This is a separate project — budget months, not weeks.

### 2. HTTP/2 not detected (REPORT §4.1.2)
`PRI * HTTP/2.0` matches no prefix. Go's HTTP/2 server path never emits an HTTP/1.1 status line. You're capturing a shrinking fraction of real traffic.

### 3. No endpoint info — but this one is cheap (REPORT §4.1.3)
No IP/port is captured, but the hooks already offer it for free:
- `sendto(fd, buf, len, flags, dest_addr, addrlen)` — `ctx->args[3]` is the **`sockaddr` pointer**, `args[4]` the length.
- `recvfrom` — same.
Reading the sockaddr in-kernel gives `src/dst ip:port` with minimal cost. **Highest ROI single change.**

### 4. No cgroup scoping (REPORT §4.1.4)
`cgroup_id` is recorded but never filtered. In a shared cluster you capture kubelet, kube-proxy, other tenants, and the agent itself. A `BPF_MAP_TYPE_HASH` of watched cgroup ids, checked before `emit_event`, is a small high-value privacy/perf win.

### 5. Missing syscall coverage (REPORT §4.2.12)
`readv`, `recvmsg`, `sendfile` are absent, and `writev` reads **only the first iovec** — Go's `net/http` splits header/body across iovecs, so most real writev requests are missed. Iterating the iovec array in-kernel (bounded, e.g. 8 entries) is doable; walking arbitrary user memory is not — hook higher instead.

### 6. `payload` is a lie (REPORT §4.1.5)
`event_t.data` is copied and shipped, but nothing persists it. Either implement opt-in body sampling, or drop the copy and keep just `data_len` — those 128 bytes of ringbuf copy cost on every event.

---

## What's not necessary / could be removed

- **`pending_reads` for read/recvfrom** — correct but doubles the probes. If you adopt `recvmsg`/kprobes for responses you can consolidate.
- **`_pad[3]` alignment field** — fine to keep (avoids unaligned data copy), just don't add more.
- **`read_args_t.buf` as `u64`** — could be a typed pointer; cosmetic only.

---

## What it could be (optional depth)

- **`bpf_get_sock_ops` / `tcp_sendmsg`-style kprobes** for TCP stats (RTT, retransmits) — syscalls can see these (REPORT §6.2).
- **A drop-counter map** so the agent reports "events lost to ringbuf pressure" — the missing half of the diagnostics story (REPORT §4.2.7).
- **`seqno` in the event** to fix fd-reuse correlation (REPORT §4.2.6) — doable in-kernel with a per-`(pid,fd)` counter.

---

## What it shouldn't be

- **A full HTTP parser in-kernel.** Keep the "fail cheap in-kernel" principle: classify + sample in kernel, parse in userspace. That's where the moat is — and where untested bugs live (REPORT §5, §8).
- **Deep user-memory walking** (`process_vm_readv`-style) for split iovecs. The real fix is higher-level hooks, not spelunking in the hot path (REPORT §4.2.12).
- **A bigger ringbuf as a fix for slow consumers** — that masks a backpressure problem; a drop counter + worker pool is the honest fix (REPORT §4.2.7).

---

## Recommendation

Match REPORT §7 Phase 2(a): do the **sockaddr capture in `sendto`/`recvfrom`** and the **cgroup filter** first — both in-kernel, cheap, and directly fix the two most embarrassing correctness gaps (no endpoints, whole-host noise). Then decide on TLS via the product question in REPORT §6.1.
