package transport

import (
	"context"
	"time"

	"github.com/emresahna/heimdall/internal/models"
	pb "github.com/emresahna/heimdall/internal/sender"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Sender interface {
	Send(ctx context.Context, batch []models.LogEntry) error
}

type GRPCSender struct {
	client pb.LogServiceClient
	cb     *CircuitBreaker
}

func NewGRPCSender(client pb.LogServiceClient, cb *CircuitBreaker) *GRPCSender {
	return &GRPCSender{
		client: client,
		cb:     cb,
	}
}

func (s *GRPCSender) Send(ctx context.Context, batch []models.LogEntry) error {
	return s.cb.Execute(ctx, func() error {
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

		sendCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()

		_, err := s.client.SendLogs(sendCtx, &pb.LogBatch{Entries: entries})
		return err
	})
}
