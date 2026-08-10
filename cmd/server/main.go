package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/emresahna/heimdall/internal/config"
	pb "github.com/emresahna/heimdall/internal/sender"
	"github.com/emresahna/heimdall/internal/server"
	"github.com/emresahna/heimdall/internal/storage"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	grpcHealthV1 "google.golang.org/grpc/health/grpc_health_v1"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}

	cfg := config.Load()
	log.Printf("starting Heimdall server version %s", version)

	db, err := storage.NewClickHouse(cfg.ClickHouseConfig)
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

	var opts []grpc.ServerOption
	if cfg.UseTLS {
		creds, err := credentials.NewServerTLSFromFile(cfg.TLSCertFile, cfg.TLSKeyFile)
		if err != nil {
			log.Fatalf("failed to load TLS credentials: %v", err)
		}
		opts = append(opts, grpc.Creds(creds))
	}

	grpcServer := grpc.NewServer(opts...)
	pb.RegisterLogServiceServer(grpcServer, server.NewLogServer(db))
	grpcHealthV1.RegisterHealthServer(grpcServer, server.NewHealthServer(db))

	httpServer := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           server.NewHttpServerWithVersion(db, version, db).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
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
		cfg.ShutdownTimeout,
	)
	defer cancelShutdown()
	_ = httpServer.Shutdown(shutdownCtx)
}
