package speedtest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// httpRTT issues a single GET and returns the round-trip time. The body is
// drained (bounded) and discarded; a non-200 response is an error.
func httpRTT(ctx context.Context, client *http.Client, url string) (time.Duration, error) {
	reqCtx, cancel := context.WithTimeout(ctx, latencyTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
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

// minAndJitter reduces latency samples (in milliseconds) to the minimum RTT
// (ping) and jitter (mean absolute difference between consecutive samples).
func minAndJitter(values []float64) (ping float64, jitter float64, err error) {
	if len(values) == 0 {
		return 0, 0, fmt.Errorf("no latency samples")
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
