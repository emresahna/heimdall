package pipeline

import "testing"

func TestParseRequestLine(t *testing.T) {
	method, path, ok := parseRequestLine([]byte("GET /api/orders HTTP/1.1\r\nHost: example"))
	if !ok {
		t.Fatalf("expected request line to parse")
	}
	if method != "GET" || path != "/api/orders" {
		t.Fatalf("unexpected parse result: %s %s", method, path)
	}
}

func TestParseResponseLine(t *testing.T) {
	status, ok := parseResponseLine([]byte("HTTP/1.1 404 Not Found\r\nServer: test"))
	if !ok {
		t.Fatalf("expected response line to parse")
	}
	if status != 404 {
		t.Fatalf("unexpected status: %d", status)
	}
}

func TestParseInvalid(t *testing.T) {
	if _, _, ok := parseRequestLine([]byte("NOTHTTP")); ok {
		t.Fatalf("expected request parse to fail")
	}
	if _, ok := parseResponseLine([]byte("BAD 200")); ok {
		t.Fatalf("expected response parse to fail")
	}
}
