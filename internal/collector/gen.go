//go:build linux

package collector

//go:generate bpf2go -no-strip -target bpf -type event_t Tracker bpf/tracker.c -- -I./bpf -I. -I/usr/include/bpf -I/usr/include -O2 -g
