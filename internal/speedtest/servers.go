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
	"strconv"
	"strings"
	"time"

	"split-vpn-webui/internal/netbind"
)

const (
	serverListURL    = "https://www.speedtest.net/api/js/servers?engine=js&limit=10"
	serverCandidates = 4
	latencySamples   = 5
	latencyTimeout   = 4 * time.Second
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

// selectServer picks the lowest-latency server among the nearest candidates,
// resolving each candidate's canonical base URL as a side effect of probing it.
func selectServer(ctx context.Context, client *http.Client) (*Server, error) {
	servers, err := fetchServers(ctx, client)
	if err != nil {
		return nil, err
	}
	var best *Server
	bestLatency := time.Duration(1<<63 - 1)
	limit := serverCandidates
	if limit > len(servers) {
		limit = len(servers)
	}
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
		if latency < bestLatency {
			bestLatency = latency
			best = s
		}
	}
	if best == nil {
		if lastErr != nil {
			return nil, fmt.Errorf("no speedtest server responded: %w", lastErr)
		}
		return nil, errNoServers
	}
	return best, nil
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
	reqCtx, cancel := context.WithTimeout(ctx, latencyTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, s.canonicalBase+"/latency.txt", nil)
	if err != nil {
		return 0, err
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("latency probe returned HTTP %d", resp.StatusCode)
	}
	return time.Since(start), nil
}

// measureLatency samples the round-trip time `samples` times and returns the
// minimum RTT (ping) and jitter (mean absolute difference between consecutive
// samples), both in milliseconds.
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
	minRTT := values[0]
	var jitterSum float64
	for i, v := range values {
		if v < minRTT {
			minRTT = v
		}
		if i > 0 {
			diff := v - values[i-1]
			if diff < 0 {
				diff = -diff
			}
			jitterSum += diff
		}
	}
	if len(values) > 1 {
		jitter = jitterSum / float64(len(values)-1)
	}
	return minRTT, jitter, nil
}
