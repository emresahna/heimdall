package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"

	"github.com/emresahna/heimdall/internal/collector"
)

func main() {
	if err := rlimit.RemoveMemlock(); err != nil {
		log.Fatalf("Locking err: %v", err)
	}

	objs := collector.TrackerObjects{}
	if err := collector.LoadTrackerObjects(&objs, nil); err != nil {
		log.Fatalf("Load objects: %v", err)
	}
	defer objs.Close()

	tp, err := link.Tracepoint("syscalls", "sys_enter_write", objs.TraceWrite, nil)
	if err != nil {
		log.Fatalf("Link tracepoint: %v", err)
	}
	defer tp.Close()

	rd, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		log.Fatalf("Open ringbuf reader: %v", err)
	}
	defer rd.Close()

	fmt.Println("Agent Started. Listening for events...")

	go func() {
		var event struct {
			Pid        uint32
			Type       uint32
			DurationNs uint64
			Payload    [200]byte
		}

		for {
			record, err := rd.Read()
			if err != nil {
				if err == ringbuf.ErrClosed {
					return
				}
				log.Printf("Read err: %v", err)
				continue
			}

			if err := binary.Read(bytes.NewBuffer(record.RawSample), binary.LittleEndian, &event); err != nil {
				log.Printf("Parse err: %v", err)
				continue
			}

			payloadStr := string(bytes.TrimRight(event.Payload[:], "\x00"))

			msgType := "UNKNOWN"
			if event.Type == 1 {
				msgType = "REQUEST (-->)"
			} else if event.Type == 2 {
				msgType = "RESPONSE (<--)"
			}

			cleanPayload := strings.ReplaceAll(payloadStr, "\n", " ")
			cleanPayload = strings.ReplaceAll(cleanPayload, "\r", "")

			fmt.Printf("[%s] PID: %d | Data: %s...\n", msgType, event.Pid, cleanPayload[:50])
		}
	}()

	stopper := make(chan os.Signal, 1)
	signal.Notify(stopper, os.Interrupt, syscall.SIGTERM)
	<-stopper
}
