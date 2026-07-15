package speedtest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMbps(t *testing.T) {
	// 1,000,000 bytes in 1s = 8 Mbps.
	if got := mbps(1_000_000, 1); got != 8 {
		t.Fatalf("mbps = %v, want 8", got)
	}
	if got := mbps(0, 1); got != 0 {
		t.Fatalf("mbps(0) = %v, want 0", got)
	}
	if got := mbps(1000, 0); got != 0 {
		t.Fatalf("mbps with zero seconds = %v, want 0", got)
	}
}

func TestClamp01(t *testing.T) {
	cases := map[float64]float64{-1: 0, 0: 0, 0.5: 0.5, 1: 1, 2: 1}
	for in, want := range cases {
		if got := clamp01(in); got != want {
			t.Fatalf("clamp01(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestUploadBodyCountsAndEOFs(t *testing.T) {
	var counter atomic.Int64
	body := newUploadBody(100, &counter)
	total, err := io.Copy(io.Discard, body)
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if total != 100 {
		t.Fatalf("copied %d bytes, want 100", total)
	}
	if counter.Load() != 100 {
		t.Fatalf("counter = %d, want 100", counter.Load())
	}
}

func TestRawLatencyURL(t *testing.T) {
	s := &Server{URL: "http://speedtest.example.com:8080/speedtest/upload.php"}
	got, err := s.rawLatencyURL()
	if err != nil {
		t.Fatalf("rawLatencyURL: %v", err)
	}
	want := "http://speedtest.example.com:8080/speedtest/latency.txt"
	if got != want {
		t.Fatalf("rawLatencyURL = %q, want %q", got, want)
	}
}

// newTestServer serves the Ookla endpoints used by the transfer code.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/speedtest/latency.txt", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "test=test\n")
	})
	mux.HandleFunc("/speedtest/upload.php", func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		io.WriteString(w, "size="+strconv.FormatInt(n, 10))
	})
	mux.HandleFunc("/speedtest/", func(w http.ResponseWriter, r *http.Request) {
		// Any random*.jpg request: stream a fixed payload.
		w.Header().Set("Content-Type", "image/jpeg")
		payload := make([]byte, 256*1024)
		w.Write(payload)
	})
	return httptest.NewServer(mux)
}

func TestResolveCanonicalAndProbe(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	s := &Server{URL: ts.URL + "/speedtest/upload.php", Host: strings.TrimPrefix(ts.URL, "http://")}
	client := ts.Client()

	if err := s.resolveCanonical(context.Background(), client); err != nil {
		t.Fatalf("resolveCanonical: %v", err)
	}
	if want := ts.URL + "/speedtest"; s.canonicalBase != want {
		t.Fatalf("canonicalBase = %q, want %q", s.canonicalBase, want)
	}
	rtt, err := s.probeLatency(context.Background(), client)
	if err != nil {
		t.Fatalf("probeLatency: %v", err)
	}
	if rtt <= 0 {
		t.Fatalf("rtt = %v, want > 0", rtt)
	}
	ping, jitter, err := s.measureLatency(context.Background(), client, 3)
	if err != nil {
		t.Fatalf("measureLatency: %v", err)
	}
	if ping <= 0 || jitter < 0 {
		t.Fatalf("ping = %v jitter = %v, want ping>0 jitter>=0", ping, jitter)
	}
}

func TestDownloadUploadCounters(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	s := &Server{canonicalBase: ts.URL + "/speedtest"}
	client := ts.Client()

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	var dl atomic.Int64
	s.download(ctx, client, &dl)
	if dl.Load() == 0 {
		t.Fatalf("download counter = 0, want > 0")
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel2()
	var ul atomic.Int64
	s.upload(ctx2, client, &ul)
	if ul.Load() == 0 {
		t.Fatalf("upload counter = 0, want > 0")
	}
}

func TestRunTransferAverages(t *testing.T) {
	// Worker adds a fixed number of bytes immediately, then idles.
	emitted := 0
	avg := runTransfer(context.Background(), 500*time.Millisecond, 2, func(Event) { emitted++ }, PhaseDownload,
		func(ctx context.Context, counter *atomic.Int64) {
			counter.Add(1_000_000)
			<-ctx.Done()
		})
	if avg <= 0 {
		t.Fatalf("avg = %v, want > 0", avg)
	}
	if emitted == 0 {
		t.Fatalf("expected at least one progress sample")
	}
}
