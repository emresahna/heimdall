//go:build linux

package collector

//go:generate bpf2go -target bpf -type event_t Tracker bpf/tracker.c -- -I./bpf -O2 -g
