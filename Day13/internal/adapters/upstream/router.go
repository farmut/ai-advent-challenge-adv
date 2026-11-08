package upstream

import (
	"log/slog"
	"net/http"
	"sort"

	"agile-consultant/internal/config"
	"agile-consultant/internal/ports"
)

// Router resolves model aliases from the config allowlist. Base URLs come
// only from the config, never from requests (no SSRF).
type Router struct {
	routes map[string]ports.Route
}

var _ ports.UpstreamRouter = (*Router)(nil)

// NewRouter builds routes. An upstream whose api_key_env is set but empty
// in the environment yields Ready=false routes and a WARN.
func NewRouter(cfg *config.Proxy, getenv func(string) string, hc *http.Client, log *slog.Logger) *Router {
	clients := map[string]*OpenAI{}
	ready := map[string]bool{}
	for name, u := range cfg.Upstreams {
		key := ""
		ready[name] = true
		if u.APIKeyEnv != "" {
			key = getenv(u.APIKeyEnv)
			if key == "" {
				ready[name] = false
				if log != nil {
					log.Warn("upstream API key is not set; its models answer 503", slog.String("upstream", name), slog.String("env", u.APIKeyEnv))
				}
			}
		}
		clients[name] = NewOpenAI(u.BaseURL, key, u.Timeout, hc)
	}
	r := &Router{routes: map[string]ports.Route{}}
	for alias, m := range cfg.Models {
		r.routes[alias] = ports.Route{
			Alias: alias, UpstreamName: m.Upstream, UpstreamModel: m.UpstreamModel, AuditModel: m.AuditModel,
			Upstream: clients[m.Upstream], Ready: ready[m.Upstream],
			Price: ports.Price{InputMicroUSDPer1M: m.Price.InputMicroUSDPer1M, OutputMicroUSDPer1M: m.Price.OutputMicroUSDPer1M, Ref: m.Price.Ref},
		}
	}
	return r
}

// Resolve returns the route for alias.
func (r *Router) Resolve(alias string) (ports.Route, bool) {
	rt, ok := r.routes[alias]
	return rt, ok
}

// Aliases returns the sorted aliases.
func (r *Router) Aliases() []string {
	out := make([]string, 0, len(r.routes))
	for a := range r.routes {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// AnyReady reports whether at least one route can be served.
func (r *Router) AnyReady() bool {
	for _, rt := range r.routes {
		if rt.Ready {
			return true
		}
	}
	return false
}
