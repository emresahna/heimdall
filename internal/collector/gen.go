//go:build linux

package collector

//go:generate bpf2go -target bpf Tracker bpf/tracker.c -- -I./bpf -O2 -g0
