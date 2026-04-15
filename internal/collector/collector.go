package collector

import (
	"context"
	"fmt"
	"log"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/emresahna/heimdall/internal/bpf"
	"github.com/emresahna/heimdall/internal/models"
)

type Collector struct {
	objs   bpf.TrackerObjects
	links  []link.Link
	reader *ringbuf.Reader
}

type LinkObj struct {
	group string
	name  string
	prog  *ebpf.Program
	opts  *link.TracepointOptions
}

func initializeLinkObjects(rawLinks []LinkObj) ([]link.Link, error) {
	links := []link.Link{}
	for _, rawLink := range rawLinks {
		pSend, err := link.Tracepoint(rawLink.group, rawLink.name, rawLink.prog, rawLink.opts)
		if err != nil {
			closeLinkObjects(links)
			return nil, err
		}
		links = append(links, pSend)
	}
	return links, nil
}

func closeLinkObjects(links []link.Link) {
	for _, link := range links {
		link.Close()
	}
}

func New() (*Collector, error) {
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("locking err: %w", err)
	}

	var objs bpf.TrackerObjects
	if err := bpf.LoadTrackerObjects(&objs, nil); err != nil {
		return nil, fmt.Errorf("load objects: %w", err)
	}

	rawLinks := []LinkObj{
		{"syscalls", "sys_enter_write", objs.TraceWriteEntry, nil},
		{"syscalls", "sys_enter_sendto", objs.TraceSendtoEntry, nil},
		{"syscalls", "sys_enter_writev", objs.TraceWritevEntry, nil},
		{"syscalls", "sys_enter_read", objs.TraceReadEntry, nil},
		{"syscalls", "sys_exit_read", objs.TraceReadExit, nil},
		{"syscalls", "sys_enter_recvfrom", objs.TraceRecvEntry, nil},
		{"syscalls", "sys_exit_recvfrom", objs.TraceRecvExit, nil},
	}

	links, err := initializeLinkObjects(rawLinks)
	if err != nil {
		objs.Close()
		return nil, err
	}

	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		objs.Close()
		closeLinkObjects(links)
		return nil, fmt.Errorf("open ringbuf reader: %w", err)
	}

	return &Collector{
		objs:   objs,
		links:  links,
		reader: reader,
	}, nil
}

func (c *Collector) Run(ctx context.Context, handler func(models.Event)) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		record, err := c.reader.Read()
		if err != nil {
			if err == ringbuf.ErrClosed {
				return nil
			}
			log.Printf("ringbuf read error: %v", err)
			continue
		}

		event, err := parseEvent(record.RawSample)
		if err != nil {
			log.Printf("parse event error: %v", err)
			continue
		}

		handler(event)
	}
}

func (c *Collector) Close() {
	c.reader.Close()
	closeLinkObjects(c.links)
	c.objs.Close()
}

// GetMap returns the BPF map by name, used for metrics collection
func (c *Collector) GetMap(name string) *ebpf.Map {
	switch name {
	case "events":
		return c.objs.Events
	case "pending_reads":
		return c.objs.PendingReads
	}
	return nil
}
