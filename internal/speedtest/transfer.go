package speedtest

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"
)

const (
	// downloadImageSize is the pixel dimension of the random JPEG requested;
	// a 4000x4000 image is ~31 MB, large enough to avoid frequent re-requests
	// on fast links while a chunked, context-aware read stops mid-file on slow
	// links at the phase deadline.
	downloadImageSize = 4000
	// uploadChunkBytes is the size of each upload POST body.
	uploadChunkBytes = 8 * 1024 * 1024
	// transferBufferSize is the per-read buffer for counting throughput.
	transferBufferSize = 64 * 1024
)

// download repeatedly fetches the server's random image, counting every byte
// received into counter, until ctx is cancelled (phase deadline reached).
func (s *Server) download(ctx context.Context, client *http.Client, counter *atomic.Int64) {
	for ctx.Err() == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.downloadURL(downloadImageSize), nil)
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

// upload repeatedly POSTs a fixed-size body, counting every byte sent into
// counter, until ctx is cancelled. A cancelled context aborts the in-flight
// request so the phase does not overshoot its deadline on slow links.
func (s *Server) upload(ctx context.Context, client *http.Client, counter *atomic.Int64) {
	for ctx.Err() == nil {
		body := newUploadBody(uploadChunkBytes, counter)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.uploadURL(), body)
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

// drain reads r in chunks, adding each read's byte count to counter, stopping on
// EOF, error, or context cancellation.
func drain(ctx context.Context, r io.Reader, counter *atomic.Int64) {
	buf := make([]byte, transferBufferSize)
	for {
		if ctx.Err() != nil {
			return
		}
		n, err := r.Read(buf)
		if n > 0 {
			counter.Add(int64(n))
		}
		if err != nil {
			return
		}
	}
}

// uploadBody is an io.Reader that yields `remaining` bytes of filler content,
// incrementing counter as the HTTP client consumes it. This lets the sampler
// observe upload progress within a single in-flight request.
type uploadBody struct {
	remaining int64
	filler    []byte
	counter   *atomic.Int64
}

func newUploadBody(size int64, counter *atomic.Int64) *uploadBody {
	filler := make([]byte, transferBufferSize)
	for i := range filler {
		filler[i] = 'A'
	}
	return &uploadBody{remaining: size, filler: filler, counter: counter}
}

func (b *uploadBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		return 0, io.EOF
	}
	n := len(p)
	if int64(n) > b.remaining {
		n = int(b.remaining)
	}
	if n > len(b.filler) {
		n = len(b.filler)
	}
	copy(p, b.filler[:n])
	b.remaining -= int64(n)
	b.counter.Add(int64(n))
	return n, nil
}
