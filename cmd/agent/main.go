package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cilium/ebpf/ringbuf"
	"github.com/emresahna/heimdall/internal/collector"
	"github.com/emresahna/heimdall/internal/config"
	"github.com/emresahna/heimdall/internal/storage"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/emresahna/heimdall/internal/sender"
)

func main() {
	cfg := config.Load()

	conn, err := grpc.NewClient(
		cfg.ServerAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Printf("did not connect: %v", err)
	}
	defer conn.Close()

	rd, err := collector.New()
	if err != nil {
		log.Printf("collector err: %v", err)
	}
	defer rd.Close()

	client := pb.NewLogServiceClient(conn)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go collector.Run(ctx, rd.Reader)

	<-ctx.Done()
	rd.Close()

	fmt.Println("Agent shutting down...")
}
