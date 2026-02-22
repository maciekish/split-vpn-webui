package latency

import (
	"errors"
	"testing"
)

func TestParseLatency(t *testing.T) {
	output := []byte("64 bytes from 1.1.1.1: icmp_seq=1 ttl=57 time=14.3 ms")
	value, err := parseLatency(output)
	if err != nil {
		t.Fatalf("parseLatency failed: %v", err)
	}
	if value != 14.3 {
		t.Fatalf("unexpected parsed latency: %v", value)
	}
}

func TestParseLatencyMissing(t *testing.T) {
	if _, err := parseLatency([]byte("no timing token here")); err == nil {
		t.Fatalf("expected parse error when time= token is missing")
	}
}

func TestSanitizeErrorPrefersTrimmedStderr(t *testing.T) {
	err := errors.New("exit status 1")
	text := sanitizeError(err, "\nfirst line\n\nsecond line\nthird line\nfourth line\n")
	if text != "first line; second line; third line" {
		t.Fatalf("unexpected sanitized error text: %q", text)
	}
}
