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
)

const maxEventData = 128

type Direction uint8

const (
	DirectionUnknown  Direction = 0
	DirectionRequest  Direction = 1
	DirectionResponse Direction = 2
)

type Event struct {
	Timestamp time.Time
	Pid       uint32
	Tid       uint32
	Fd        int32
	CgroupID  uint64
	Direction Direction
	Data      []byte
}

type Collector struct {
	objs       TrackerObjects
	tpWrite    link.Link
	tpSend     link.Link
	tpReadEnt  link.Link
	tpReadExit link.Link
	tpRecvEnt  link.Link
	tpRecvExit link.Link
	reader     *ringbuf.Reader
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

	tpSend, err := link.Tracepoint("syscalls", "sys_enter_sendto", objs.TraceSendtoEntry, nil)
	if err != nil {
		tpWrite.Close()
		objs.Close()
		return nil, fmt.Errorf("link sys_enter_sendto: %w", err)
	}

	tpReadEnt, err := link.Tracepoint("syscalls", "sys_enter_read", objs.TraceReadEntry, nil)
	if err != nil {
		tpSend.Close()
		tpWrite.Close()
		objs.Close()
		return nil, fmt.Errorf("link sys_enter_read: %w", err)
	}

	tpReadExit, err := link.Tracepoint("syscalls", "sys_exit_read", objs.TraceReadExit, nil)
	if err != nil {
		tpReadEnt.Close()
		tpSend.Close()
		tpWrite.Close()
		objs.Close()
		return nil, fmt.Errorf("link sys_exit_read: %w", err)
	}

	tpRecvEnt, err := link.Tracepoint("syscalls", "sys_enter_recvfrom", objs.TraceRecvEntry, nil)
	if err != nil {
		tpReadExit.Close()
		tpReadEnt.Close()
		tpSend.Close()
		tpWrite.Close()
		objs.Close()
		return nil, fmt.Errorf("link sys_enter_recvfrom: %w", err)
	}

	tpRecvExit, err := link.Tracepoint("syscalls", "sys_exit_recvfrom", objs.TraceRecvExit, nil)
	if err != nil {
		tpRecvEnt.Close()
		tpReadExit.Close()
		tpReadEnt.Close()
		tpSend.Close()
		tpWrite.Close()
		objs.Close()
		return nil, fmt.Errorf("link sys_exit_recvfrom: %w", err)
	}

	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		tpRecvExit.Close()
		tpRecvEnt.Close()
		tpReadExit.Close()
		tpReadEnt.Close()
		tpSend.Close()
		tpWrite.Close()
		objs.Close()
		return nil, fmt.Errorf("open ringbuf reader: %w", err)
	}

	return &Collector{
		objs:       objs,
		tpWrite:    tpWrite,
		tpSend:     tpSend,
		tpReadEnt:  tpReadEnt,
		tpReadExit: tpReadExit,
		tpRecvEnt:  tpRecvEnt,
		tpRecvExit: tpRecvExit,
		reader:     reader,
	}, nil
}

func (c *Collector) Run(ctx context.Context, handler func(Event)) error {
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
	if c.reader != nil {
		c.reader.Close()
	}
	if c.tpRecvExit != nil {
		c.tpRecvExit.Close()
	}
	if c.tpRecvEnt != nil {
		c.tpRecvEnt.Close()
	}
	if c.tpReadExit != nil {
		c.tpReadExit.Close()
	}
	if c.tpReadEnt != nil {
		c.tpReadEnt.Close()
	}
	if c.tpSend != nil {
		c.tpSend.Close()
	}
	if c.tpWrite != nil {
		c.tpWrite.Close()
	}
	c.objs.Close()
}

type bpfEvent struct {
	TsNs      uint64
	CgroupID  uint64
	Pid       uint32
	Tid       uint32
	Fd        int32
	DataLen   uint32
	EventType uint8
	_         [3]byte
	Data      [maxEventData]byte
}

func parseEvent(raw []byte) (Event, error) {
	var evt bpfEvent
	if err := binary.Read(bytes.NewReader(raw), binary.LittleEndian, &evt); err != nil {
		return Event{}, fmt.Errorf("binary read: %w", err)
	}

	dataLen := int(evt.DataLen)
	if dataLen > maxEventData {
		dataLen = maxEventData
	}
	data := make([]byte, dataLen)
	copy(data, evt.Data[:dataLen])

	return Event{
		Timestamp: time.Unix(0, int64(evt.TsNs)),
		Pid:       evt.Pid,
		Tid:       evt.Tid,
		Fd:        evt.Fd,
		CgroupID:  evt.CgroupID,
		Direction: Direction(evt.EventType),
		Data:      bytes.TrimRight(data, "\x00"),
	}, nil
}
