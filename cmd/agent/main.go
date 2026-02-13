package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/emresahna/heimdall/internal/collector"
	"github.com/emresahna/heimdall/internal/config"
	"github.com/emresahna/heimdall/internal/correlation"
	"github.com/emresahna/heimdall/internal/enrichment"
	"github.com/emresahna/heimdall/internal/pipeline"
	pb "github.com/emresahna/heimdall/internal/sender"
	"github.com/emresahna/heimdall/internal/transport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	cfg := config.Load()

	if cfg.ServerAddr == "" {
		log.Fatal("SERVER_ADDR is required")
	}

	conn, err := grpc.NewClient(
		cfg.ServerAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("failed to connect to server: %v", err)
	}
	defer conn.Close()

	collectorInstance, err := collector.New()
	if err != nil {
		log.Fatalf("collector error: %v", err)
	}
	defer collectorInstance.Close()

	client := pb.NewLogServiceClient(conn)
	sender := transport.NewGRPCSender(client)
	diagnostics := pipeline.NewDiagnostics()
	batcher := pipeline.NewBatcher(
		cfg.Agent.BatchSize,
		cfg.Agent.FlushInterval,
		cfg.Agent.MaxQueue,
		sender,
		diagnostics,
	)
	correlator := correlation.NewCorrelator(cfg.Agent.CorrelatorTTL)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	enricher, err := enrichment.NewEnricher(ctx, cfg.Agent.K8sEnrich, cfg.Agent.NodeName)
	if err != nil {
		log.Fatalf("enricher error: %v", err)
	}

	processor := pipeline.NewProcessor(
		ctx,
		correlator,
		enricher,
		batcher,
		cfg.Agent.NodeName,
		cfg.Agent.HTTPSampleBytes,
		diagnostics,
	)

	go batcher.Run(ctx)
	go processor.RunMaintenance(ctx, cfg.Agent.CorrelatorTTL)
	go pipeline.StartDiagnosticsReporter(ctx, diagnostics, cfg.Agent.DiagnosticsInterval)
	go func() {
		if err := collectorInstance.Run(ctx, processor.HandleEvent); err != nil {
			log.Printf("collector stopped: %v", err)
		}
	}()

	<-ctx.Done()
	collectorInstance.Close()
	log.Println("agent shutting down")
}
