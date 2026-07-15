// Package speedtest measures per-interface throughput using either the Ookla
// (speedtest.net) or Netflix fast.com protocol. Each test can be bound to a
// specific network interface (SO_BINDTODEVICE) so it measures a particular WAN
// or VPN tunnel, and it reports live progress through a callback so results can
// be streamed to the browser as the test runs.
package speedtest

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// Provider selects which speed-test backend to use.
type Provider string

const (
	ProviderOokla Provider = "ookla"
	ProviderFast  Provider = "fast"
)

// ParseProvider normalizes a provider string, defaulting to Ookla.
func ParseProvider(value string) (Provider, bool) {
	switch value {
	case "", string(ProviderOokla):
		return ProviderOokla, true
	case string(ProviderFast):
		return ProviderFast, true
	default:
		return ProviderOokla, false
	}
}

// Phase identifies which stage of the test a progress event belongs to.
type Phase string

const (
	PhaseServer   Phase = "server"
	PhasePing     Phase = "ping"
	PhaseDownload Phase = "download"
	PhaseUpload   Phase = "upload"
	PhaseDone     Phase = "done"
	PhaseError    Phase = "error"
)

// Event is a single progress update emitted during a test. Fields are only
// populated when relevant to the phase; the JSON tags match what the frontend
// speedtest controller consumes.
type Event struct {
	Phase Phase `json:"phase"`

	ServerName     string  `json:"serverName,omitempty"`
	ServerSponsor  string  `json:"serverSponsor,omitempty"`
	ServerCountry  string  `json:"serverCountry,omitempty"`
	ServerDistance float64 `json:"serverDistanceKm,omitempty"`

	PingMS   float64 `json:"pingMs,omitempty"`
	JitterMS float64 `json:"jitterMs,omitempty"`

	// DownloadMbps/UploadMbps carry the instantaneous rate during their
	// respective phases and the final average in the done event.
	DownloadMbps float64 `json:"downloadMbps,omitempty"`
	UploadMbps   float64 `json:"uploadMbps,omitempty"`

	// Progress is the 0..1 completion fraction of the current phase.
	Progress float64 `json:"progress,omitempty"`

	Message string `json:"message,omitempty"`
}

// Options configures a single test run.
type Options struct {
	// Interface is the network device to bind egress to (e.g. "eth8",
	// "wg-sv-foo"). Empty binds to the default route.
	Interface string
	// Label is a human-readable name for the target, echoed for logging only.
	Label string
	// Provider selects the backend; empty defaults to Ookla.
	Provider Provider

	DownloadDuration time.Duration
	UploadDuration   time.Duration
	// Parallel is the number of concurrent connections per transfer phase.
	Parallel int
}

func (o *Options) withDefaults() {
	if o.Provider == "" {
		o.Provider = ProviderOokla
	}
	if o.DownloadDuration <= 0 {
		o.DownloadDuration = 10 * time.Second
	}
	if o.UploadDuration <= 0 {
		o.UploadDuration = 8 * time.Second
	}
	if o.Parallel <= 0 {
		o.Parallel = 4
	}
}

// sampleInterval is how often instantaneous throughput is reported during
// download and upload phases.
const sampleInterval = 300 * time.Millisecond

// targetInfo describes the selected server/endpoint for display.
type targetInfo struct {
	Name       string
	Sponsor    string
	Country    string
	DistanceKm float64
}

// target is a resolved endpoint that can be latency-probed and driven for
// download/upload. Both the Ookla server and the fast.com endpoint implement it,
// so the measurement pipeline is provider-agnostic.
type target interface {
	info() targetInfo
	measureLatency(ctx context.Context, client *http.Client, samples int) (ping float64, jitter float64, err error)
	download(ctx context.Context, client *http.Client, counter *atomic.Int64)
	upload(ctx context.Context, client *http.Client, counter *atomic.Int64)
}

// Run executes a full test (target selection, ping, download, upload) bound to
// the configured interface, invoking emit for every progress update. It returns
// an error if a fatal step fails (target discovery/selection); transient
// transfer errors are tolerated as long as some data moved. emit is always
// called from Run's own goroutine, so callers need no synchronization.
func Run(ctx context.Context, opts Options, emit func(Event)) error {
	opts.withDefaults()

	client := newClient(opts.Interface)

	tgt, err := selectTarget(ctx, client, opts.Provider)
	if err != nil {
		return err
	}
	nfo := tgt.info()
	emit(Event{
		Phase:          PhaseServer,
		ServerName:     nfo.Name,
		ServerSponsor:  nfo.Sponsor,
		ServerCountry:  nfo.Country,
		ServerDistance: nfo.DistanceKm,
	})

	ping, jitter, err := tgt.measureLatency(ctx, client, latencySamples)
	if err != nil {
		return err
	}
	emit(Event{Phase: PhasePing, PingMS: ping, JitterMS: jitter, Progress: 1})

	downloadMbps := runTransfer(ctx, opts.DownloadDuration, opts.Parallel, emit, PhaseDownload, tgt.download, client)
	uploadMbps := runTransfer(ctx, opts.UploadDuration, opts.Parallel, emit, PhaseUpload, tgt.upload, client)

	emit(Event{
		Phase:          PhaseDone,
		ServerName:     nfo.Name,
		ServerSponsor:  nfo.Sponsor,
		ServerCountry:  nfo.Country,
		ServerDistance: nfo.DistanceKm,
		PingMS:         ping,
		JitterMS:       jitter,
		DownloadMbps:   downloadMbps,
		UploadMbps:     uploadMbps,
	})
	return nil
}

// selectTarget dispatches target selection to the requested provider.
func selectTarget(ctx context.Context, client *http.Client, provider Provider) (target, error) {
	switch provider {
	case ProviderFast:
		return selectFastTarget(ctx, client)
	default:
		return selectServer(ctx, client)
	}
}

// runTransfer fans out `parallel` workers that push bytes through a shared
// counter for `duration`, sampling the counter on a ticker to emit live rates.
// It returns the average throughput in Mbps over the whole window.
func runTransfer(ctx context.Context, duration time.Duration, parallel int, emit func(Event), phase Phase, worker func(context.Context, *http.Client, *atomic.Int64), client *http.Client) float64 {
	runCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	var counter atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		workerDone := make(chan struct{}, parallel)
		for i := 0; i < parallel; i++ {
			go func() {
				worker(runCtx, client, &counter)
				workerDone <- struct{}{}
			}()
		}
		for i := 0; i < parallel; i++ {
			<-workerDone
		}
	}()

	start := time.Now()
	lastBytes := int64(0)
	lastTime := start
	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			total := counter.Load()
			elapsed := time.Since(start).Seconds()
			return mbps(total, elapsed)
		case now := <-ticker.C:
			current := counter.Load()
			interval := now.Sub(lastTime).Seconds()
			instant := mbps(current-lastBytes, interval)
			lastBytes = current
			lastTime = now
			progress := clamp01(now.Sub(start).Seconds() / duration.Seconds())
			evt := Event{Phase: phase, Progress: progress}
			if phase == PhaseDownload {
				evt.DownloadMbps = instant
			} else {
				evt.UploadMbps = instant
			}
			emit(evt)
		}
	}
}

func mbps(bytes int64, seconds float64) float64 {
	if seconds <= 0 || bytes <= 0 {
		return 0
	}
	return float64(bytes) * 8 / seconds / 1_000_000
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// errNoServers is returned when a provider's endpoint list is empty.
var errNoServers = fmt.Errorf("no speed-test servers reachable through the selected interface")
