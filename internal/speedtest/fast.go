package speedtest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

const (
	fastMainURL   = "https://fast.com/"
	fastAPIFormat = "https://api.fast.com/netflix/speedtest/v2?https=true&token=%s&urlCount=%d"
	// fastURLCount is how many OCA targets to request; download/upload workers
	// spread across them, which is how fast.com reaches high throughput.
	fastURLCount = 5
	// fastDownloadRange is the byte range requested per download call; a
	// context-aware read stops mid-stream at the phase deadline.
	fastDownloadRange = 25 * 1024 * 1024
)

var (
	fastAppScriptRe = regexp.MustCompile(`/app-[0-9a-f]+\.js`)
	fastTokenRe     = regexp.MustCompile(`token:"([^"]+)"`)
)

// fastTarget is a resolved fast.com endpoint set (multiple Netflix OCA URLs).
type fastTarget struct {
	urls    []fastURL
	name    string
	country string
	next    atomic.Uint64
}

// fastURL splits an OCA target into its path base and query so a byte range can
// be inserted into the path (fast.com rejects the range appended after the
// query string).
type fastURL struct {
	base  string // e.g. https://host/speedtest
	query string // e.g. c=sg&n=14061&t=...
}

func (u fastURL) rangeURL(bytes int64) string {
	return fmt.Sprintf("%s/range/0-%d?%s", u.base, bytes, u.query)
}

type fastResponse struct {
	Client struct {
		Location fastLocation `json:"location"`
	} `json:"client"`
	Targets []struct {
		URL      string       `json:"url"`
		Location fastLocation `json:"location"`
	} `json:"targets"`
}

type fastLocation struct {
	City    string `json:"city"`
	Country string `json:"country"`
}

// selectFastTarget fetches a fast.com session token and its OCA target list.
func selectFastTarget(ctx context.Context, client *http.Client) (target, error) {
	token, err := fetchFastToken(ctx, client)
	if err != nil {
		return nil, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	apiURL := fmt.Sprintf(fastAPIFormat, token, fastURLCount)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch fast.com targets: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fast.com API returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, fmt.Errorf("read fast.com targets: %w", err)
	}
	var parsed fastResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse fast.com targets: %w", err)
	}

	tgt := &fastTarget{}
	for _, t := range parsed.Targets {
		u := splitFastURL(t.URL)
		if u.base == "" {
			continue
		}
		tgt.urls = append(tgt.urls, u)
		if tgt.name == "" {
			tgt.name = t.Location.City
			tgt.country = t.Location.Country
		}
	}
	if len(tgt.urls) == 0 {
		return nil, errNoServers
	}
	if tgt.name == "" {
		tgt.name = parsed.Client.Location.City
		tgt.country = parsed.Client.Location.Country
	}
	return tgt, nil
}

// fetchFastToken scrapes the session token embedded in fast.com's app script.
func fetchFastToken(ctx context.Context, client *http.Client) (string, error) {
	page, err := fastGet(ctx, client, fastMainURL)
	if err != nil {
		return "", fmt.Errorf("load fast.com: %w", err)
	}
	scriptPath := fastAppScriptRe.FindString(page)
	if scriptPath == "" {
		return "", fmt.Errorf("fast.com app script not found")
	}
	script, err := fastGet(ctx, client, "https://fast.com"+scriptPath)
	if err != nil {
		return "", fmt.Errorf("load fast.com script: %w", err)
	}
	match := fastTokenRe.FindStringSubmatch(script)
	if len(match) < 2 {
		return "", fmt.Errorf("fast.com token not found")
	}
	return match[1], nil
}

func fastGet(ctx context.Context, client *http.Client, url string) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// splitFastURL separates the OCA URL into path base and query string.
func splitFastURL(raw string) fastURL {
	idx := strings.IndexByte(raw, '?')
	if idx < 0 {
		return fastURL{base: raw}
	}
	return fastURL{base: raw[:idx], query: raw[idx+1:]}
}

func (t *fastTarget) info() targetInfo {
	return targetInfo{Name: t.name, Sponsor: "Netflix (fast.com)", Country: t.country}
}

// pick returns the next target URL, rotating across OCAs to spread load.
func (t *fastTarget) pick() fastURL {
	i := t.next.Add(1) - 1
	return t.urls[int(i%uint64(len(t.urls)))]
}

func (t *fastTarget) measureLatency(ctx context.Context, client *http.Client, samples int) (ping float64, jitter float64, err error) {
	if samples < 1 {
		samples = 1
	}
	probeURL := t.urls[0].rangeURL(0)
	values := make([]float64, 0, samples)
	for i := 0; i < samples; i++ {
		if ctx.Err() != nil {
			break
		}
		rtt, probeErr := httpRTT(ctx, client, probeURL)
		if probeErr != nil {
			err = probeErr
			continue
		}
		values = append(values, float64(rtt.Microseconds())/1000)
	}
	if len(values) == 0 {
		if err == nil {
			err = fmt.Errorf("fast.com latency measurement failed")
		}
		return 0, 0, err
	}
	return minAndJitter(values)
}

func (t *fastTarget) download(ctx context.Context, client *http.Client, counter *atomic.Int64) {
	for ctx.Err() == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.pick().rangeURL(fastDownloadRange), nil)
		if err != nil {
			return
		}
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		drain(ctx, resp.Body, counter)
		resp.Body.Close()
	}
}

func (t *fastTarget) upload(ctx context.Context, client *http.Client, counter *atomic.Int64) {
	for ctx.Err() == nil {
		body := newUploadBody(uploadChunkBytes, counter)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.pick().rangeURL(0), body)
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/octet-stream")
		req.ContentLength = uploadChunkBytes
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
	}
}
