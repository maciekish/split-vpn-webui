package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"split-vpn-webui/internal/speedtest"
)

// speedtestAcquireWait bounds how long a request waits for an in-flight test to
// finish before giving up. It only needs to cover the brief handoff when a user
// switches provider (the stream is closed and reopened); a genuinely concurrent
// test from another client still fails after this window.
const speedtestAcquireWait = 3 * time.Second

// handleSpeedtestStream runs an Ookla speed test bound to the requested
// interface and streams live progress to the browser as Server-Sent Events.
// The connection stays open for the duration of the test and closes when the
// test finishes; if the client disconnects, the test is cancelled via the
// request context.
func (s *Server) handleSpeedtestStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}

	iface := strings.TrimSpace(r.URL.Query().Get("iface"))
	label := strings.TrimSpace(r.URL.Query().Get("label"))
	if iface == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "iface query parameter is required"})
		return
	}
	if !s.isKnownInterface(iface) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown interface"})
		return
	}
	provider, ok := speedtest.ParseProvider(strings.TrimSpace(r.URL.Query().Get("provider")))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown provider"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	emit := func(evt speedtest.Event) {
		data, err := json.Marshal(evt)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	// Only one test may run at a time; concurrent tests would compete for
	// bandwidth and produce meaningless numbers. Wait briefly rather than
	// failing instantly, so switching provider (which closes and reopens the
	// stream) does not race the previous run's asynchronous guard release.
	if !s.acquireSpeedtest(r.Context(), speedtestAcquireWait) {
		fmt.Fprintf(w, "retry: 10000\n\n")
		flusher.Flush()
		emit(speedtest.Event{Phase: speedtest.PhaseError, Message: "Another speed test is already running."})
		return
	}
	defer s.speedtestActive.Store(false)

	fmt.Fprintf(w, "retry: 10000\n\n")
	flusher.Flush()

	opts := speedtest.Options{Interface: iface, Label: label, Provider: provider}
	if err := speedtest.Run(r.Context(), opts, emit); err != nil {
		if r.Context().Err() != nil {
			return // client disconnected mid-test
		}
		emit(speedtest.Event{Phase: speedtest.PhaseError, Message: err.Error()})
	}
}

// acquireSpeedtest tries to claim the single-run slot, waiting up to `wait` for
// a previous run to release it (or until the client disconnects). Returns true
// once claimed, false on timeout or cancellation.
func (s *Server) acquireSpeedtest(ctx context.Context, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for {
		if s.speedtestActive.CompareAndSwap(false, true) {
			return true
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			return false
		}
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			return false
		}
	}
}

// isKnownInterface reports whether device matches a currently tracked WAN or VPN
// interface, preventing arbitrary SO_BINDTODEVICE targets from the query string.
func (s *Server) isKnownInterface(device string) bool {
	snapshot := s.stats.Snapshot()
	for _, iface := range snapshot.Interfaces {
		if iface.Interface == device {
			return true
		}
	}
	return false
}
