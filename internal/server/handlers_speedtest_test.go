package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"split-vpn-webui/internal/stats"
)

func TestHandleSpeedtestStreamRequiresInterface(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/speedtest/stream", nil)
	rec := httptest.NewRecorder()
	s.handleSpeedtestStream(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHandleSpeedtestStreamRejectsUnknownInterface(t *testing.T) {
	s := &Server{stats: stats.NewCollector("eth8", time.Second, 10)}
	req := httptest.NewRequest(http.MethodGet, "/api/speedtest/stream?iface=nonexistent0", nil)
	rec := httptest.NewRecorder()
	s.handleSpeedtestStream(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestIsKnownInterface(t *testing.T) {
	collector := stats.NewCollector("eth8", time.Second, 10)
	collector.ConfigureInterfaces("eth8", map[string]string{"tunnel": "wg-sv-test"})
	s := &Server{stats: collector}
	if !s.isKnownInterface("eth8") {
		t.Fatalf("expected eth8 to be known")
	}
	if !s.isKnownInterface("wg-sv-test") {
		t.Fatalf("expected wg-sv-test to be known")
	}
	if s.isKnownInterface("eth99") {
		t.Fatalf("expected eth99 to be unknown")
	}
}
