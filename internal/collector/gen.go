//go:build linux

package collector

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target native Tracker bpf/tracker.c -- -I/usr/include/bpf -I/usr/include
