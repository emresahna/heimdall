package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"

	pb "github.com/emresahna/heimdall/internal/sender"
	"github.com/emresahna/heimdall/internal/storage"
	"google.golang.org/grpc"
)

var db *storage.DB

type server struct {
	pb.UnimplementedLogServiceServer
}

func (s *server) SendLogs(ctx context.Context, req *pb.LogBatch) (*pb.Response, error) {
	var logs []storage.LogEntry

	for _, entry := range req.Entries {
		logs = append(logs, storage.LogEntry{
			Timestamp:  entry.Timestamp.AsTime(),
			Pid:        entry.Pid,
			Type:       entry.Type,
			Payload:    entry.Payload,
			DurationNs: entry.DurationNs,
		})
	}

	go func(data []storage.LogEntry) {
		if err := db.InsertBatch(data); err != nil {
			log.Printf("Failed to write to DB: %v", err)
		} else {
			fmt.Printf("%d log received.\n", len(data))
		}
	}(logs)

	return &pb.Response{Success: true, Message: "OK"}, nil
}

func main() {
	dbAddr := os.Getenv("CLICKHOUSE_ADDR")
	dbUser := os.Getenv("CLICKHOUSE_USER")
	dbPass := os.Getenv("CLICKHOUSE_PASSWORD")
	dbName := os.Getenv("CLICKHOUSE_DB")

	var err error
	db, err = storage.NewClickHouse(storage.Config{
		Addr:     dbAddr,
		Database: dbName,
		User:     dbUser,
		Password: dbPass,
	})
	if err != nil {
		log.Fatalf("DB Connection Error: %v", err)
	}

	if err := db.Migrate(); err != nil {
		log.Fatalf("Migration Error: %v", err)
	}

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	pb.RegisterLogServiceServer(s, &server{})

	fmt.Println("gRPC Server listening on port 50051...")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
