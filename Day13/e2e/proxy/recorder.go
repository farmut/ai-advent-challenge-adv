package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync"
)

// Recorder is a reverse proxy that records request bodies and Authorization
// headers before forwarding them to the real upstream (fake,
// mlx_lm.server or any OpenAI-compatible API).
type Recorder struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []string
	auths  []string
}

// NewRecorder forwards to target (scheme://host[:port][/base]); the
// request path is appended to the target path and Host is the target host.
func NewRecorder(target *url.URL) *Recorder {
	rec := &Recorder{}
	rp := &httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) { pr.SetURL(target) }}
	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(b))
		rec.mu.Lock()
		rec.bodies = append(rec.bodies, string(b))
		rec.auths = append(rec.auths, r.Header.Get("Authorization"))
		rec.mu.Unlock()
		rp.ServeHTTP(w, r)
	}))
	return rec
}

// Count returns the number of recorded requests.
func (r *Recorder) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.bodies)
}

// Since returns bodies recorded after the first n requests.
func (r *Recorder) Since(n int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n >= len(r.bodies) {
		return nil
	}
	return append([]string(nil), r.bodies[n:]...)
}

// Auths returns the Authorization headers of all recorded requests ("" when
// absent).
func (r *Recorder) Auths() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.auths...)
}
