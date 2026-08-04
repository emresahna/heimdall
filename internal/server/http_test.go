package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeChecker struct {
	healthy bool
}

func (f fakeChecker) IsHealthy() bool {
	return f.healthy
}

func TestReadyzHealthy(t *testing.T) {
	s := NewHttpServer(nil, fakeChecker{healthy: true})

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestReadyzUnhealthy(t *testing.T) {
	s := NewHttpServer(nil, fakeChecker{healthy: false})

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestHealthzHealthy(t *testing.T) {
	s := NewHttpServer(nil, fakeChecker{healthy: true})

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestHealthzUnhealthy(t *testing.T) {
	s := NewHttpServer(nil, fakeChecker{healthy: false})

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestHealthzNoChecker(t *testing.T) {
	s := NewHttpServer(nil)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}
