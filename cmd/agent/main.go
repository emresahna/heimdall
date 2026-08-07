package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/emresahna/heimdall/internal/collector"
	"github.com/emresahna/heimdall/internal/config"
	"github.com/emresahna/heimdall/internal/correlation"
	"github.com/emresahna/heimdall/internal/enrichment"
	"github.com/emresahna/heimdall/internal/pipeline"
	pb "github.com/emresahna/heimdall/internal/sender"
	"github.com/emresahna/heimdall/internal/transport"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
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
	log.Printf("starting Heimdall agent version %s", version)

	var opts []grpc.DialOption
	if cfg.UseTLS {
		creds, err := credentials.NewClientTLSFromFile(cfg.TLSCAFile, "")
		if err != nil {
			log.Fatalf("failed to load TLS credentials: %v", err)
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.NewClient(cfg.ServerAddr, opts...)
	if err != nil {
		log.Fatalf("failed to connect to server: %v", err)
	}
	defer conn.Close()

	coll, err := collector.New()
	if err != nil {
		log.Fatalf("collector error: %v", err)
	}
	defer coll.Close()

	cb := transport.NewCircuitBreakerWithComponent(cfg.CircuitBreakerConfig.CBThreshold, cfg.CircuitBreakerConfig.CBResetTimeout, "sender")
	client := pb.NewLogServiceClient(conn)
	sender := transport.NewGRPCSender(client, cb)
	diagnostics := pipeline.NewDiagnostics()
	batcher := pipeline.NewBatcher(cfg.BatcherConfig, sender, diagnostics, cfg.NodeName)
	correlator := correlation.NewCorrelator(cfg.CorrelatorConfig)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	enricher, err := enrichment.NewEnricher(ctx, cfg.K8sEnrich, cfg.NodeName)
	if err != nil {
		log.Fatalf("enricher error: %v", err)
	}

	processor := pipeline.NewProcessor(
		ctx,
		correlator,
		enricher,
		batcher,
		cfg.NodeName,
		cfg.HTTPSampleBytes,
		nil,
		diagnostics,
	)

	go func() {
		log.Printf("Starting metrics server on port %s", cfg.MetricsPort)
		metricsServer := &http.Server{
			Addr:              ":" + cfg.MetricsPort,
			Handler:           promhttp.Handler(),
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       120 * time.Second,
		}
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("metrics server error: %v", err)
		}
	}()

	// Start BPF map metrics reporter if we have access to the collector maps
	reporter := collector.NewBPFMapMetricsReporter(
		collector.MapInfo{Name: "events", Map: coll.GetMap("events")},
		collector.MapInfo{Name: "pending_reads", Map: coll.GetMap("pending_reads"), Count: collector.CountHashMapKeys},
	)
	go reporter.Start(ctx, 10*time.Second)

	go batcher.Run(ctx)
	go processor.RunMaintenance(ctx, cfg.TTL)
	go pipeline.StartDiagnosticsReporter(ctx, diagnostics, cfg.DiagnosticsInterval)
	go func() {
		if err := coll.Run(ctx, processor.HandleEvent); err != nil {
			log.Printf("collector stopped: %v", err)
		}
	}()

	<-ctx.Done()
	coll.Close()
	log.Println("agent shutting down")
}
