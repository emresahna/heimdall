// +build ignore

#include <linux/types.h> 
#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

char __license[] SEC("license") = "Dual MIT/GPL";

typedef __u32 u32;
typedef __u64 u64;

struct trace_event_raw_sys_enter {
    unsigned long long ent;
    long int id;
    unsigned long args[6];
};

struct http_event_t {
    u32 pid;
    u32 type;
    u64 duration_ns;
    unsigned char payload[200];
};

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1 << 24);
} events SEC(".maps");

static __always_inline int is_http(const char *buf) {
    if (buf[0] == 'G' && buf[1] == 'E' && buf[2] == 'T' && buf[3] == ' ') return 1;
    if (buf[0] == 'P' && buf[1] == 'O' && buf[2] == 'S' && buf[3] == 'T') return 1;
    if (buf[0] == 'H' && buf[1] == 'T' && buf[2] == 'T' && buf[3] == 'P') return 2;
    return 0;
}

SEC("tracepoint/syscalls/sys_enter_write")
int trace_write(struct trace_event_raw_sys_enter *ctx) {
    u64 id = bpf_get_current_pid_tgid();
    u32 pid = id >> 32;

    const char *buf = (const char *)ctx->args[1];
    
    char prefix[4] = {0};
    bpf_probe_read_user(&prefix, sizeof(prefix), buf);

    int type = is_http(prefix);
    if (type == 0) {
        return 0;
    }

    struct http_event_t *event;
    event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event) {
        return 0;
    }

    event->pid = pid;
    event->type = type;
    event->duration_ns = bpf_ktime_get_ns();
    
    bpf_probe_read_user(&event->payload, sizeof(event->payload), buf);

    bpf_ringbuf_submit(event, 0);

    return 0;
}