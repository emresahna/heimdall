package redact

import (
	"bytes"
	"strings"
	"testing"
)

type stubRedactor struct {
	replacement string
}

func (s stubRedactor) Redact(b []byte) []byte {
	return []byte(s.replacement)
}

func TestRedactorInterfaceContract(t *testing.T) {
	red := stubRedactor{replacement: "[REDACTED]"}
	got := string(red.Redact([]byte("Authorization: Bearer secret")))
	if got != "[REDACTED]" {
		t.Fatalf("expected replacement, got %q", got)
	}
}

func TestRedactorReturnsReplacementBytes(t *testing.T) {
	red := stubRedactor{replacement: "x"}
	in := bytes.Repeat([]byte("payload"), 16)
	if !strings.Contains(string(red.Redact(in)), "x") {
		t.Fatalf("expected replacement bytes present")
	}
}
