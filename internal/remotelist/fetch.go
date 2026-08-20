package remotelist

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const fetchUserAgent = "split-vpn-webui-remote-list/1"

// fetchResponse is the outcome of one conditional HTTP download.
type fetchResponse struct {
	// NotModified is true when the server answered 304 and no body was sent.
	NotModified  bool
	Body         []byte
	ETag         string
	LastModified string
}

// fetchList downloads a list body, using the stored validators so an unchanged
// source costs a single 304 response instead of a full transfer.
func fetchList(ctx context.Context, client *http.Client, listURL string, state fetchState) (fetchResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return fetchResponse{}, err
	}
	request.Header.Set("User-Agent", fetchUserAgent)
	request.Header.Set("Accept", "text/plain, */*")
	if strings.TrimSpace(state.ETag) != "" {
		request.Header.Set("If-None-Match", state.ETag)
	}
	if strings.TrimSpace(state.LastModified) != "" {
		request.Header.Set("If-Modified-Since", state.LastModified)
	}

	response, err := client.Do(request)
	if err != nil {
		return fetchResponse{}, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		_ = response.Body.Close()
	}()

	if response.StatusCode == http.StatusNotModified {
		return fetchResponse{
			NotModified:  true,
			ETag:         firstNonEmpty(response.Header.Get("ETag"), state.ETag),
			LastModified: firstNonEmpty(response.Header.Get("Last-Modified"), state.LastModified),
		}, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fetchResponse{}, fmt.Errorf("unexpected HTTP status %s", response.Status)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, MaxBodyBytes+1))
	if err != nil {
		return fetchResponse{}, fmt.Errorf("read body: %w", err)
	}
	if len(body) > MaxBodyBytes {
		return fetchResponse{}, fmt.Errorf("response exceeds the %d byte limit", MaxBodyBytes)
	}
	return fetchResponse{
		Body:         body,
		ETag:         strings.TrimSpace(response.Header.Get("ETag")),
		LastModified: strings.TrimSpace(response.Header.Get("Last-Modified")),
	}, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
