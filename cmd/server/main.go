package main

import (
	"context"
	"fmt"
	"log"
	"net"

	"github.com/emresahna/heimdall/internal/config"
	"github.com/emresahna/heimdall/internal/storage"
	"google.golang.org/grpc"

	pb "github.com/emresahna/heimdall/internal/sender"
)

type Server struct {
	pb.UnimplementedLogServiceServer
	DB *storage.DB
}

func main() {
	cfg := config.Load()

	var err error
	db, err := storage.NewClickHouse(storage.Config{
		Addr:     cfg.ClickHouseConfig.Addr,
		Database: cfg.ClickHouseConfig.DB,
		User:     cfg.ClickHouseConfig.User,
		Password: cfg.ClickHouseConfig.Password,
	})
	if err != nil {
		log.Fatalf("DB connection error: %v", err)
	}

	if err := db.Migrate(); err != nil {
		log.Fatalf("migration error: %v", err)
	}

	lis, err := net.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	sender := &Server{DB: db}
	grpcServer := grpc.NewServer()
	pb.RegisterLogServiceServer(grpcServer, sender)

	log.Printf("gRPC Server listening on port %s...\n", cfg.Port)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func (s *Server) SendLogs(ctx context.Context, req *pb.LogBatch) (*pb.Response, error) {
	logs := make([]storage.LogEntry, 0, len(req.Entries))
	for _, entry := range req.Entries {
		logs = append(logs, storage.LogEntry{
			Timestamp:  entry.Timestamp.AsTime(),
			Pid:        entry.Pid,
			Type:       entry.Type,
			Payload:    entry.Payload,
			DurationNs: entry.DurationNs,
			Status:     entry.Status,
			Method:     entry.Method,
			Path:       entry.Path,
		})
	}

	go func(data []storage.LogEntry) {
		if err := s.DB.InsertBatch(data); err != nil {
			log.Printf("Failed to write to DB: %v", err)
		} else {
			fmt.Printf("%d log received.\n", len(data))
		}
	}(logs)

	return &pb.Response{Success: true, Message: "OK"}, nil
}
