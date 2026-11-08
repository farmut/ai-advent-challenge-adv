package proxy

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// UpstreamFake is the deterministic fake upstream; every other upstream
// (mlx, openai-compat, ...) is a real model whose output cases are n/a.
const UpstreamFake = "fake"

// IsFakeUpstream reports whether upstream is the deterministic fake.
func IsFakeUpstream(upstream string) bool { return upstream == UpstreamFake }

// defaultUpstreams are always shown in the summary (missing runs yield
// empty columns); other upstreams found in meta.upstream follow them.
var defaultUpstreams = []string{UpstreamFake, "mlx"}

// UpstreamOrder returns the summary column groups: fake and mlx first
// (even without a run), then every other upstream found in runs, sorted.
func UpstreamOrder(runs map[string]*RunData) []string {
	out := append([]string(nil), defaultUpstreams...)
	seen := map[string]bool{}
	for _, u := range out {
		seen[u] = true
	}
	var extra []string
	for u := range runs {
		if !seen[u] {
			extra = append(extra, u)
		}
	}
	sort.Strings(extra)
	return append(out, extra...)
}

// CompatBaseURL parses the openai-compat base URL (E2E_BASE_URL, e.g.
// http://127.0.0.1:11434/v1). Like upstreams.*.base_url in the proxy
// config it must be http(s) with a host and without userinfo, query or
// fragment.
func CompatBaseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("is required (e.g. http://127.0.0.1:11434/v1)")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("must be an http(s) URL")
	}
	switch {
	case u.User != nil:
		return nil, fmt.Errorf("must not contain userinfo: %s", RedactURL(u))
	case u.RawQuery != "" || u.ForceQuery:
		return nil, errors.New("must not contain a query")
	case u.Fragment != "" || strings.Contains(raw, "#"):
		return nil, errors.New("must not contain a fragment")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u, nil
}

// RedactURL returns u without userinfo, query and fragment (for reports).
func RedactURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	c := *u
	c.User, c.RawQuery, c.ForceQuery, c.Fragment, c.RawFragment = nil, "", false, "", ""
	return c.String()
}
