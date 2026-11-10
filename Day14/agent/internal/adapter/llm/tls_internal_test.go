package llm

import (
	"crypto/tls"
	"io"
	"net/http"
	"testing"
)

func tlsOf(t *testing.T, c *http.Client) *tls.Config {
	t.Helper()
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr.TLSClientConfig == nil {
		t.Fatalf("transport has no TLS config: %#v", c.Transport)
	}
	return tr.TLSClientConfig
}

// GigaChat used to run with InsecureSkipVerify; verification must now be on
// for every provider, with TLS 1.2 as the floor.
func TestNewClient_TLSVerificationAlwaysOn(t *testing.T) {
	for _, p := range []string{ProviderOpenAI, ProviderOpenRouter, ProviderGigaChat} {
		c, err := NewClient(Config{Provider: p, Log: io.Discard})
		if err != nil {
			t.Fatal(err)
		}
		clients := []*http.Client{c.httpClient}
		if p == ProviderGigaChat {
			if c.gcClient == nil {
				t.Fatal("gigachat client not built")
			}
			clients = append(clients, c.gcClient)
		}
		for _, hc := range clients {
			cfg := tlsOf(t, hc)
			if cfg.InsecureSkipVerify {
				t.Errorf("%s: InsecureSkipVerify must be false", p)
			}
			if cfg.MinVersion != tls.VersionTLS12 {
				t.Errorf("%s: MinVersion = %x, want TLS1.2", p, cfg.MinVersion)
			}
		}
	}
}
