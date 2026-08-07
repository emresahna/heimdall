package server

import (
	"io"
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
	s := NewHttpServerWithVersion(nil, "v0.1.0", fakeChecker{healthy: true})

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if got, want := string(body), "ok\nversion: v0.1.0\n"; got != want {
		t.Fatalf("expected body %q, got %q", want, got)
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
