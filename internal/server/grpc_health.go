package server

import (
	"context"

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

// NewHealthServer creates a new health server. If no checker is provided,
// it will always return SERVING status.
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
	status := grpc_health_v1.HealthCheckResponse_SERVING
	if s.checker != nil && !s.checker.IsHealthy() {
		status = grpc_health_v1.HealthCheckResponse_NOT_SERVING
	}

	if err := stream.Send(&grpc_health_v1.HealthCheckResponse{
		Status: status,
	}); err != nil {
		return err
	}

	// Keep stream open
	<-stream.Context().Done()
	return nil
}
