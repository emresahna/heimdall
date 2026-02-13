package transport

import (
	"context"
	"time"

	pb "github.com/emresahna/heimdall/internal/sender"
	"github.com/emresahna/heimdall/internal/telemetry"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Sender interface {
	Send(ctx context.Context, batch []telemetry.LogEntry) error
}

type GRPCSender struct {
	client pb.LogServiceClient
}

func NewGRPCSender(client pb.LogServiceClient) *GRPCSender {
	return &GRPCSender{client: client}
}

func (s *GRPCSender) Send(ctx context.Context, batch []telemetry.LogEntry) error {
	entries := make([]*pb.LogEntry, 0, len(batch))
	for _, entry := range batch {
		entries = append(entries, &pb.LogEntry{
			Timestamp:   timestamppb.New(entry.Timestamp),
			Pid:         entry.Pid,
			Tid:         entry.Tid,
			Fd:          entry.Fd,
			CgroupId:    entry.CgroupID,
			Type:        entry.Type,
			Payload:     entry.Payload,
			DurationNs:  entry.DurationNs,
			Status:      entry.Status,
			Method:      entry.Method,
			Path:        entry.Path,
			Node:        entry.Node,
			Namespace:   entry.Namespace,
			Pod:         entry.Pod,
			Container:   entry.Container,
			ContainerId: entry.ContainerID,
		})
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	_, err := s.client.SendLogs(ctx, &pb.LogBatch{Entries: entries})
	return err
}
