package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/emresahna/heimdall/internal/config"
	pb "github.com/emresahna/heimdall/internal/sender"
	"github.com/emresahna/heimdall/internal/server"
	"github.com/emresahna/heimdall/internal/storage"
	"google.golang.org/grpc"
)

func main() {
	cfg := config.Load()

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

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	lis, err := net.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterLogServiceServer(grpcServer, server.NewGrpcServer(db))

	httpServer := &http.Server{
		Addr:    ":" + cfg.HTTPPort,
		Handler: server.NewHttpServer(db).Handler(),
	}

	go func() {
		log.Printf("gRPC server listening on port %s", cfg.Port)
		if err := grpcServer.Serve(lis); err != nil {
			log.Printf("gRPC server error: %v", err)
		}
	}()

	go func() {
		log.Printf("HTTP server listening on port %s", cfg.HTTPPort)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	<-ctx.Done()
	grpcServer.GracefulStop()

	shutdownCtx, cancelShutdown := context.WithTimeout(
		context.Background(),
		cfg.HTTPShutdownTimeout,
	)
	defer cancelShutdown()
	_ = httpServer.Shutdown(shutdownCtx)
}
