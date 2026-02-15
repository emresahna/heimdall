// +build ignore

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

typedef unsigned int u32;
typedef unsigned long long u64;
typedef int s32;

char __license[] SEC("license") = "Dual MIT/GPL";

#define MAX_DATA 128
#define EVENT_REQUEST 1
#define EVENT_RESPONSE 2

struct event_t {
	u64 ts_ns;
	u64 cgroup_id;
	u32 pid;
	u32 tid;
	s32 fd;
	u32 data_len;
	u8 event_type;
	u8 _pad[3];
	char data[MAX_DATA];
} __attribute__((preserve_access_index));

struct read_args_t {
	u64 buf;
	s32 fd;
};

struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 1 << 24);
} events SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 65535);
	__type(key, u32);
	__type(value, struct read_args_t);
} pending_reads SEC(".maps");

static __always_inline int is_http_request(const char *buf) {
	if (buf[0] == 'G' && buf[1] == 'E' && buf[2] == 'T' && buf[3] == ' ') return 1;
	if (buf[0] == 'P' && buf[1] == 'O' && buf[2] == 'S' && buf[3] == 'T') return 1;
	if (buf[0] == 'P' && buf[1] == 'U' && buf[2] == 'T' && buf[3] == ' ') return 1;
	if (buf[0] == 'D' && buf[1] == 'E' && buf[2] == 'L' && buf[3] == 'E') return 1;
	if (buf[0] == 'P' && buf[1] == 'A' && buf[2] == 'T' && buf[3] == 'C') return 1;
	if (buf[0] == 'H' && buf[1] == 'E' && buf[2] == 'A' && buf[3] == 'D') return 1;
	if (buf[0] == 'O' && buf[1] == 'P' && buf[2] == 'T' && buf[3] == 'I') return 1;
	return 0;
}

static __always_inline int is_http_response(const char *buf) {
	if (buf[0] == 'H' && buf[1] == 'T' && buf[2] == 'T' && buf[3] == 'P') return 1;
	return 0;
}

static __always_inline int emit_event(const char *buf, size_t count, s32 fd, u8 event_type) {
	u64 id = bpf_get_current_pid_tgid();
	u32 pid = id >> 32;
	u32 tid = (u32)id;
	u32 len;

	if (count > MAX_DATA - 1) {
		len = MAX_DATA - 1;
	} else {
		len = (u32)count;
	}

	if (len == 0) {
		return 0;
	}

	struct event_t *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
	if (!e) {
		return 0;
	}

	e->ts_ns = bpf_ktime_get_ns();
	e->cgroup_id = bpf_get_current_cgroup_id();
	e->pid = pid;
	e->tid = tid;
	e->fd = fd;
	e->data_len = len;
	e->event_type = event_type;

	if (bpf_probe_read_user(e->data, len, buf) != 0) {
		bpf_ringbuf_discard(e, 0);
		return 0;
	}
	e->data[len] = 0;

	bpf_ringbuf_submit(e, 0);
	return 0;
}

SEC("tracepoint/syscalls/sys_enter_write")
int trace_write_entry(struct trace_event_raw_sys_enter *ctx) {
	s32 fd = (s32)ctx->args[0];
	const char *buf = (const char *)ctx->args[1];
	size_t count = (size_t)ctx->args[2];
	char prefix[8] = {};

	if (count < 4) {
		return 0;
	}
	if (bpf_probe_read_user(prefix, sizeof(prefix), buf) != 0) {
		return 0;
	}
	if (!is_http_request(prefix)) {
		return 0;
	}

	return emit_event(buf, count, fd, EVENT_REQUEST);
}

SEC("tracepoint/syscalls/sys_enter_sendto")
int trace_sendto_entry(struct trace_event_raw_sys_enter *ctx) {
	s32 fd = (s32)ctx->args[0];
	const char *buf = (const char *)ctx->args[1];
	size_t count = (size_t)ctx->args[2];
	char prefix[8] = {};

	if (count < 4) {
		return 0;
	}
	if (bpf_probe_read_user(prefix, sizeof(prefix), buf) != 0) {
		return 0;
	}
	if (!is_http_request(prefix)) {
		return 0;
	}

	return emit_event(buf, count, fd, EVENT_REQUEST);
}

SEC("tracepoint/syscalls/sys_enter_writev")
int trace_writev_entry(struct trace_event_raw_sys_enter *ctx) {
    s32 fd = (s32)ctx->args[0];
    void *iov_ptr = (void *)ctx->args[1];
    size_t vlen = (size_t)ctx->args[2];
    
    struct iovec iov;
    char prefix[8] = {};

    if (vlen == 0) {
        return 0;
    }

    if (bpf_probe_read_user(&iov, sizeof(iov), iov_ptr) != 0) {
        return 0;
    }

    if (iov.iov_len < 4) {
        return 0;
    }

    if (bpf_probe_read_user(prefix, sizeof(prefix), iov.iov_base) != 0) {
        return 0;
    }

	int is_req = is_http_request(prefix);
	int is_res = is_http_response(prefix);

    if (is_req || is_res) {
        return emit_event((const char *)iov.iov_base, iov.iov_len, fd, is_req ? EVENT_REQUEST : EVENT_RESPONSE);
    }

    return 0;
}

SEC("tracepoint/syscalls/sys_enter_read")
int trace_read_entry(struct trace_event_raw_sys_enter *ctx) {
	u64 id = bpf_get_current_pid_tgid();
	u32 tid = (u32)id;
	struct read_args_t args = {};

	args.fd = (s32)ctx->args[0];
	args.buf = (u64)ctx->args[1];
	bpf_map_update_elem(&pending_reads, &tid, &args, BPF_ANY);
	return 0;
}

SEC("tracepoint/syscalls/sys_exit_read")
int trace_read_exit(struct trace_event_raw_sys_exit *ctx) {
	u64 id = bpf_get_current_pid_tgid();
	u32 tid = (u32)id;
	struct read_args_t *args = bpf_map_lookup_elem(&pending_reads, &tid);
	long ret = ctx->ret;
	char prefix[8] = {};

	if (!args) {
		return 0;
	}

	if (ret <= 0) {
		bpf_map_delete_elem(&pending_reads, &tid);
		return 0;
	}

	if (ret < 4) {
		bpf_map_delete_elem(&pending_reads, &tid);
		return 0;
	}

	if (bpf_probe_read_user(prefix, sizeof(prefix), (const void *)args->buf) != 0) {
		bpf_map_delete_elem(&pending_reads, &tid);
		return 0;
	}

	if (is_http_response(prefix)) {
		emit_event((const char *)args->buf, (size_t)ret, args->fd, EVENT_RESPONSE);
	}

	bpf_map_delete_elem(&pending_reads, &tid);
	return 0;
}

SEC("tracepoint/syscalls/sys_enter_recvfrom")
int trace_recv_entry(struct trace_event_raw_sys_enter *ctx) {
	u64 id = bpf_get_current_pid_tgid();
	u32 tid = (u32)id;
	struct read_args_t args = {};

	args.fd = (s32)ctx->args[0];
	args.buf = (u64)ctx->args[1];
	bpf_map_update_elem(&pending_reads, &tid, &args, BPF_ANY);
	return 0;
}

SEC("tracepoint/syscalls/sys_exit_recvfrom")
int trace_recv_exit(struct trace_event_raw_sys_exit *ctx) {
	u64 id = bpf_get_current_pid_tgid();
	u32 tid = (u32)id;
	struct read_args_t *args = bpf_map_lookup_elem(&pending_reads, &tid);
	long ret = ctx->ret;
	char prefix[8] = {};

	if (!args) {
		return 0;
	}

	if (ret <= 0) {
		bpf_map_delete_elem(&pending_reads, &tid);
		return 0;
	}

	if (ret < 4) {
		bpf_map_delete_elem(&pending_reads, &tid);
		return 0;
	}

	if (bpf_probe_read_user(prefix, sizeof(prefix), (const void *)args->buf) != 0) {
		bpf_map_delete_elem(&pending_reads, &tid);
		return 0;
	}

	if (is_http_response(prefix)) {
		emit_event((const char *)args->buf, (size_t)ret, args->fd, EVENT_RESPONSE);
	}

	bpf_map_delete_elem(&pending_reads, &tid);
	return 0;
}
