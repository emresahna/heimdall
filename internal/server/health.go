package server

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// HealthChecker interface for health status
type HealthChecker interface {
	IsHealthy() bool
}

// HealthServer implements grpc.health.v1.Health service
type HealthServer struct {
	grpc_health_v1.UnimplementedHealthServer
	checker HealthChecker
}

func NewHealthServer(checker ...HealthChecker) *HealthServer {
	var c HealthChecker
	if len(checker) > 0 {
		c = checker[0]
	}
	return &HealthServer{checker: c}
}

func (s *HealthServer) Check(ctx context.Context, req *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	if s.checker != nil && !s.checker.IsHealthy() {
		return &grpc_health_v1.HealthCheckResponse{
			Status: grpc_health_v1.HealthCheckResponse_NOT_SERVING,
		}, nil
	}
	return &grpc_health_v1.HealthCheckResponse{
		Status: grpc_health_v1.HealthCheckResponse_SERVING,
	}, nil
}

func (s *HealthServer) Watch(req *grpc_health_v1.HealthCheckRequest, stream grpc_health_v1.Health_WatchServer) error {
	// Send initial serving status
	if err := stream.Send(&grpc_health_v1.HealthCheckResponse{
		Status: grpc_health_v1.HealthCheckResponse_SERVING,
	}); err != nil {
		return err
	}

	// Keep stream open - no status changes in this implementation
	<-stream.Context().Done()
	return nil
}

// RegisterHealthService registers the health service on a gRPC server
func RegisterHealthService(s *grpc.Server, healthServer *HealthServer) {
	grpc_health_v1.RegisterHealthServer(s, healthServer)
}

// GrpcHealthServer wraps the standard health service for use without custom checker
type GrpcHealthServer struct {
	*grpc_health_v1.UnimplementedHealthServer
}

func NewGrpcHealthServer() *GrpcHealthServer {
	return &GrpcHealthServer{}
}

func (s *GrpcHealthServer) Check(ctx context.Context, req *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{
		Status: grpc_health_v1.HealthCheckResponse_SERVING,
	}, nil
}

func (s *GrpcHealthServer) Watch(req *grpc_health_v1.HealthCheckRequest, stream grpc_health_v1.Health_WatchServer) error {
	if err := stream.Send(&grpc_health_v1.HealthCheckResponse{
		Status: grpc_health_v1.HealthCheckResponse_SERVING,
	}); err != nil {
		return err
	}
	<-stream.Context().Done()
	return nil
}

// RegisterStandardHealth registers the standard gRPC health service
func RegisterStandardHealth(s *grpc.Server) {
	grpc_health_v1.RegisterHealthServer(s, &GrpcHealthServer{})
}
