package server

import (
	"context"
	"log"

	pb "github.com/emresahna/heimdall/internal/sender"
	"github.com/emresahna/heimdall/internal/storage"
	"github.com/emresahna/heimdall/internal/telemetry"
)

type GrpcServer struct {
	pb.UnimplementedLogServiceServer

	DB *storage.DB
}

func NewGrpcServer(db *storage.DB) *GrpcServer {
	return &GrpcServer{DB: db}
}

func (s *GrpcServer) SendLogs(ctx context.Context, req *pb.LogBatch) (*pb.Response, error) {
	logs := make([]telemetry.LogEntry, 0, len(req.Entries))
	for _, entry := range req.Entries {
		logs = append(logs, telemetry.LogEntry{
			Timestamp:   entry.Timestamp.AsTime(),
			Pid:         entry.Pid,
			Tid:         entry.Tid,
			Fd:          entry.Fd,
			CgroupID:    entry.CgroupId,
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
			ContainerID: entry.ContainerId,
		})
	}

	if err := s.DB.InsertBatch(logs); err != nil {
		log.Printf("failed to write to DB: %v", err)
		return &pb.Response{Success: false, Message: "insert failed"}, err
	}

	return &pb.Response{Success: true, Message: "OK"}, nil
}
