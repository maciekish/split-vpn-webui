package speedtest

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"split-vpn-webui/internal/netbind"
)

const (
	serverListURL    = "https://www.speedtest.net/api/js/servers?engine=js&limit=10"
	serverCandidates = 5
	latencySamples   = 5
	latencyTimeout   = 4 * time.Second

	// After latency filtering, the fastest-by-throughput of the top
	// selectionProbeCount servers is chosen, each probed for
	// selectionProbeDuration. This guards against landing on a low-latency but
	// download-throttled server.
	selectionProbeCount    = 3
	selectionProbeDuration = 1200 * time.Millisecond
	selectionProbeParallel = 2
)

// Server is a single speedtest.net endpoint with its resolved canonical base
// URL (the real HTTPS host behind the sponsor's redirect).
type Server struct {
	ID       string
	Name     string
	Country  string
	Sponsor  string
	Host     string
	URL      string
	Distance float64

	canonicalBase string // e.g. https://server-24742.prod.hosts.ooklaserver.net:8080/speedtest
}

// newClient builds an HTTP client whose sockets bind to the given interface.
// HTTP/2 is disabled so each concurrent transfer worker uses its own TCP
// connection, which is required to saturate a link accurately.
func newClient(iface string) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if control := netbind.Control(iface); control != nil {
		dialer.Control = control
	}
	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		MaxConnsPerHost:     0,
		ForceAttemptHTTP2:   false,
		TLSNextProto:        map[string]func(string, *tls.Conn) http.RoundTripper{},
		TLSClientConfig:     &tls.Config{},
	}
	return &http.Client{Transport: transport}
}

type rawServer struct {
	URL      string      `json:"url"`
	Host     string      `json:"host"`
	Name     string      `json:"name"`
	Country  string      `json:"country"`
	Sponsor  string      `json:"sponsor"`
	ID       string      `json:"id"`
	Distance json.Number `json:"distance"`
}

// fetchServers retrieves the nearest servers for the client's egress IP. Because
// the client is interface-bound, this returns servers near the interface's exit
// (for a VPN, near the tunnel's exit location).
func fetchServers(ctx context.Context, client *http.Client) ([]*Server, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, serverListURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "split-vpn-webui-speedtest")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch server list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server list returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, fmt.Errorf("read server list: %w", err)
	}
	var raw []rawServer
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse server list: %w", err)
	}
	servers := make([]*Server, 0, len(raw))
	for _, r := range raw {
		if strings.TrimSpace(r.URL) == "" {
			continue
		}
		dist, _ := strconv.ParseFloat(r.Distance.String(), 64)
		servers = append(servers, &Server{
			ID:       r.ID,
			Name:     r.Name,
			Country:  r.Country,
			Sponsor:  r.Sponsor,
			Host:     r.Host,
			URL:      r.URL,
			Distance: dist,
		})
	}
	if len(servers) == 0 {
		return nil, errNoServers
	}
	return servers, nil
}

func (s *Server) info() targetInfo {
	return targetInfo{Name: s.Name, Sponsor: s.Sponsor, Country: s.Country, DistanceKm: s.Distance}
}

// selectServer resolves and latency-probes the nearest candidates, then runs a
// short download probe on the lowest-latency few and returns the fastest. This
// avoids picking a nearby but throttled server, which otherwise produces
// misleadingly low results.
func selectServer(ctx context.Context, client *http.Client) (target, error) {
	servers, err := fetchServers(ctx, client)
	if err != nil {
		return nil, err
	}
	limit := serverCandidates
	if limit > len(servers) {
		limit = len(servers)
	}

	type candidate struct {
		server  *Server
		latency time.Duration
	}
	var reachable []candidate
	var lastErr error
	for i := 0; i < limit; i++ {
		s := servers[i]
		if err := s.resolveCanonical(ctx, client); err != nil {
			lastErr = err
			continue
		}
		latency, err := s.probeLatency(ctx, client)
		if err != nil {
			lastErr = err
			continue
		}
		reachable = append(reachable, candidate{server: s, latency: latency})
	}
	if len(reachable) == 0 {
		if lastErr != nil {
			return nil, fmt.Errorf("no speedtest server responded: %w", lastErr)
		}
		return nil, errNoServers
	}

	sort.Slice(reachable, func(i, j int) bool { return reachable[i].latency < reachable[j].latency })
	probeLimit := selectionProbeCount
	if probeLimit > len(reachable) {
		probeLimit = len(reachable)
	}

	best := reachable[0].server // lowest-latency fallback
	var bestRate float64
	for i := 0; i < probeLimit; i++ {
		if ctx.Err() != nil {
			break
		}
		rate := probeDownloadThroughput(ctx, client, reachable[i].server)
		if rate > bestRate {
			bestRate = rate
			best = reachable[i].server
		}
	}
	return best, nil
}

// probeDownloadThroughput runs a brief download against the server and returns
// the observed Mbps, used only to compare candidate servers during selection.
func probeDownloadThroughput(ctx context.Context, client *http.Client, s *Server) float64 {
	return runTransfer(ctx, selectionProbeDuration, selectionProbeParallel, func(Event) {}, PhaseDownload, s.download, client)
}

// resolveCanonical follows the sponsor host's redirect to the real HTTPS host
// (which carries a valid certificate) and records the canonical base directory.
func (s *Server) resolveCanonical(ctx context.Context, client *http.Client) error {
	latencyURL, err := s.rawLatencyURL()
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, latencyTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, latencyURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("latency probe for %s returned HTTP %d", s.Host, resp.StatusCode)
	}
	eff := resp.Request.URL
	base := *eff
	base.Path = path.Dir(eff.Path)
	base.RawQuery = ""
	s.canonicalBase = strings.TrimRight(base.String(), "/")
	return nil
}

// rawLatencyURL derives the sponsor latency.txt URL from the server's upload URL.
func (s *Server) rawLatencyURL() (string, error) {
	u, err := url.Parse(s.URL)
	if err != nil {
		return "", fmt.Errorf("invalid server url %q: %w", s.URL, err)
	}
	u.Path = path.Join(path.Dir(u.Path), "latency.txt")
	u.RawQuery = ""
	return u.String(), nil
}

func (s *Server) downloadURL(size int) string {
	return fmt.Sprintf("%s/random%dx%d.jpg?nocache=%d", s.canonicalBase, size, size, time.Now().UnixNano())
}

func (s *Server) uploadURL() string {
	return s.canonicalBase + "/upload.php"
}

// probeLatency issues a single latency request and returns the round-trip time.
func (s *Server) probeLatency(ctx context.Context, client *http.Client) (time.Duration, error) {
	return httpRTT(ctx, client, s.canonicalBase+"/latency.txt")
}

// measureLatency samples the round-trip time `samples` times and returns the
// minimum RTT (ping) and jitter, both in milliseconds.
func (s *Server) measureLatency(ctx context.Context, client *http.Client, samples int) (ping float64, jitter float64, err error) {
	if samples < 1 {
		samples = 1
	}
	values := make([]float64, 0, samples)
	for i := 0; i < samples; i++ {
		if ctx.Err() != nil {
			break
		}
		rtt, probeErr := s.probeLatency(ctx, client)
		if probeErr != nil {
			err = probeErr
			continue
		}
		values = append(values, float64(rtt.Microseconds())/1000)
	}
	if len(values) == 0 {
		if err == nil {
			err = fmt.Errorf("latency measurement failed")
		}
		return 0, 0, err
	}
	return minAndJitter(values)
}
