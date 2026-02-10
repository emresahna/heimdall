//go:build linux

package collector

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/emresahna/heimdall/internal/storage"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Event struct {
	StartTS    uint64
	DurationNS uint64
	PID        uint32
	Type       uint32
	Status     uint32
	Method     string
	Path       string
}

type Collector struct {
	objs    TrackerObjects
	tpWrite link.Link
	tpRead  link.Link
	Reader  *ringbuf.Reader
}

type Collector struct {
	reader *ringbuf.Reader
	client pb.LogServiceClient
}

func New() (*Collector, error) {
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("locking err: %w", err)
	}

	var objs TrackerObjects
	if err := LoadTrackerObjects(&objs, nil); err != nil {
		return nil, fmt.Errorf("load objects: %w", err)
	}

	tpWrite, err := link.Tracepoint("syscalls", "sys_enter_write", objs.TraceWriteEntry, nil)
	if err != nil {
		objs.Close()
		return nil, fmt.Errorf("link sys_enter_write: %w", err)
	}

	tpRead, err := link.Tracepoint("syscalls", "sys_exit_read", objs.TraceReadExit, nil)
	if err != nil {
		tpWrite.Close()
		objs.Close()
		return nil, fmt.Errorf("link sys_exit_read: %w", err)
	}

	rd, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		tpRead.Close()
		tpWrite.Close()
		objs.Close()
		return nil, fmt.Errorf("open ringbuf reader: %w", err)
	}

	return &Collector{
		objs:    objs,
		tpWrite: tpWrite,
		tpRead:  tpRead,
		Reader:  rd,
	}, nil
}

func (c *Collector) Run(ctx context.Context) {
	var batchBuffer []storage.LogEntry

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		record, err := c.reader.Read()
		if err != nil {
			if err == ringbuf.ErrClosed {
				return
			}
			log.Printf("Read err: %v", err)
			continue
		}

		event, err := ParseEvent(record.RawSample)
		if err != nil {
			log.Printf("Parse err: %v", err)
			continue
		}

		batchBuffer = append(batchBuffer, storage.LogEntry{
			Timestamp:  time.Unix(0, int64(event.StartTS)), // Use nanosecond timestamp from BPF
			Pid:        event.PID,
			Type:       fmt.Sprintf("%d", event.Type), // TODO: Map internal types to strings properly if needed
			Status:     event.Status,
			Method:     event.Method,
			Path:       event.Path,
			DurationNs: event.DurationNS,
		})

		if len(batchBuffer) >= 10 {
			c.sendRPC(batchBuffer)
			batchBuffer = batchBuffer[:0]
		}
	}
}

func (c *Collector) sendRPC(logs []storage.LogEntry) {
	protoLogs := make([]*pb.LogEntry, 0, len(logs))
	for _, l := range logs {
		protoLogs = append(protoLogs, &pb.LogEntry{
			Timestamp:  timestamppb.New(l.Timestamp),
			Pid:        l.Pid,
			Type:       l.Type,
			Payload:    l.Payload,
			DurationNs: l.DurationNs,
			Status:     l.Status,
			Method:     l.Method,
			Path:       l.Path,
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := client.SendLogs(ctx, &pb.LogBatch{Entries: protoLogs})
	if err != nil {
		log.Printf("gRPC error: %v", err)
	} else {
		fmt.Printf("%d log sent.\n", len(logs))
	}
}

func (c *Collector) Close() {
	if c.Reader != nil {
		c.Reader.Close()
	}
	if c.tpRead != nil {
		c.tpRead.Close()
	}
	if c.tpWrite != nil {
		c.tpWrite.Close()
	}
	c.objs.Close()
}

func LoadTrackerObjects(objs *TrackerObjects, opts *link.TracepointOptions) error {
	spec, err := LoadTracker()
	if err != nil {
		return fmt.Errorf("loading spec: %w", err)
	}
	return spec.LoadAndAssign(objs, nil)
}

func ParseEvent(raw []byte) (*Event, error) {
	var cEvent struct {
		StartTS    uint64
		DurationNS uint64
		PID        uint32
		Type       uint32
		Status     uint32
		Method     [8]byte
		Path       [128]byte
	}

	if err := binary.Read(bytes.NewReader(raw), binary.LittleEndian, &cEvent); err != nil {
		return nil, fmt.Errorf("binary read: %w", err)
	}

	return &Event{
		StartTS:    cEvent.StartTS,
		DurationNS: cEvent.DurationNS,
		PID:        cEvent.PID,
		Type:       cEvent.Type,
		Status:     cEvent.Status,
		Method:     string(bytes.TrimRight(cEvent.Method[:], "\x00")),
		Path:       string(bytes.TrimRight(cEvent.Path[:], "\x00")),
	}, nil
}
