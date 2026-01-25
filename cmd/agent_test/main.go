package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/emresahna/heimdall/internal/collector"
	"github.com/emresahna/heimdall/internal/storage"

	pb "github.com/emresahna/heimdall/internal/sender"
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

	serverAddr := os.Getenv("SERVER_ADDR")

	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewLogServiceClient(conn)

	go func() {
		var event struct {
			Pid        uint32
			Type       uint32
			DurationNs uint64
			Payload    [200]byte
		}

		var batchBuffer []storage.LogEntry

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

			batchBuffer = append(batchBuffer, storage.LogEntry{
				Timestamp:  time.Now(),
				Pid:        event.Pid,
				Type:       msgType,
				Payload:    cleanPayload,
				DurationNs: event.DurationNs,
			})

			if len(batchBuffer) >= 10 {
				sendRPC(client, batchBuffer)
				batchBuffer = batchBuffer[:0]
			}
		}
	}()

	stopper := make(chan os.Signal, 1)
	signal.Notify(stopper, os.Interrupt, syscall.SIGTERM)
	<-stopper
}

func sendRPC(client pb.LogServiceClient, logs []storage.LogEntry) {
	var protoLogs []*pb.LogEntry
	for _, l := range logs {
		protoLogs = append(protoLogs, &pb.LogEntry{
			Timestamp:  l.Timestamp.UnixNano(),
			Pid:        l.Pid,
			Type:       l.Type,
			Payload:    l.Payload,
			DurationNs: l.DurationNs,
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
