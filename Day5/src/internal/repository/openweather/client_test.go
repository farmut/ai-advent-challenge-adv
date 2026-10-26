package openweather_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"weatherbot/internal/domain"
	"weatherbot/internal/repository/openweather"
)

const okBody = `{"name":"Москва","sys":{"country":"RU"},"main":{"temp":12.4,"feels_like":9.6,"humidity":71},
"weather":[{"description":"переменная облачность"}],"wind":{"speed":4.2,"deg":315},"cod":200}`

func TestClientCurrent(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		wantErr  error
		wantCity string
	}{
		{name: "ok", status: http.StatusOK, body: okBody, wantCity: "Москва"},
		{name: "not found", status: http.StatusNotFound, body: `{"cod":"404","message":"city not found"}`, wantErr: domain.ErrCityNotFound},
		{name: "bad key", status: http.StatusUnauthorized, body: `{"cod":401}`, wantErr: domain.ErrProviderAuth},
		{name: "rate limited", status: http.StatusTooManyRequests, body: `{}`, wantErr: domain.ErrRateLimited},
		{name: "server error", status: http.StatusInternalServerError, body: `oops`, wantErr: domain.ErrProviderUnavailable},
		{name: "bad gateway", status: http.StatusBadGateway, body: ``, wantErr: domain.ErrProviderUnavailable},
		{name: "unexpected status", status: http.StatusTeapot, body: `{}`, wantErr: domain.ErrProviderUnavailable},
		{name: "broken json", status: http.StatusOK, body: `{"name":`, wantErr: domain.ErrProviderUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				if r.Method != http.MethodGet || r.URL.Path != "/data/2.5/weather" || q.Get("q") != "Москва" ||
					q.Get("appid") != "test-key" || q.Get("units") != "metric" || q.Get("lang") != "ru" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			t.Cleanup(srv.Close)

			c := openweather.NewClient(srv.URL, "test-key", srv.Client())
			got, err := c.Current(t.Context(), "Москва")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Current() err = %v, want %v", err, tt.wantErr)
			}
			if got.City != tt.wantCity {
				t.Errorf("Current() city = %q, want %q", got.City, tt.wantCity)
			}
		})
	}
}

func TestClientCurrentMapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, okBody)
	}))
	t.Cleanup(srv.Close)

	got, err := openweather.NewClient(srv.URL, "test-key", srv.Client()).Current(t.Context(), "Москва")
	if err != nil {
		t.Fatalf("Current() err = %v", err)
	}
	want := domain.Weather{
		City: "Москва", Country: "RU", TempC: 12.4, FeelsLikeC: 9.6,
		Description: "переменная облачность", Humidity: 71, WindSpeed: 4.2, WindDeg: 315,
	}
	if got != want {
		t.Errorf("Current() = %+v, want %+v", got, want)
	}
}

func TestClientCurrentEscapesCity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("q") != "a&appid=x" || len(q["appid"]) != 1 || q.Get("appid") != "test-key" {
			t.Errorf("query was not escaped: %s", r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, okBody)
	}))
	t.Cleanup(srv.Close)

	if _, err := openweather.NewClient(srv.URL, "test-key", srv.Client()).Current(t.Context(), "a&appid=x"); err != nil {
		t.Fatalf("Current() err = %v", err)
	}
}

func TestClientCurrentEscapesApostrophe(t *testing.T) {
	tests := []struct {
		name    string
		city    string
		wantRaw string
	}{
		{name: "ascii", city: "N'Djamena", wantRaw: "q=N%27Djamena"},
		{name: "typographic", city: "N’Djamena", wantRaw: "q=N%E2%80%99Djamena"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				if q.Get("q") != tt.city || len(q["appid"]) != 1 || q.Get("appid") != "test-key" {
					t.Errorf("unexpected query: %s", r.URL.RawQuery)
				}
				if !strings.Contains(r.URL.RawQuery, tt.wantRaw) {
					t.Errorf("RawQuery = %s, want it to contain %s", r.URL.RawQuery, tt.wantRaw)
				}
				_, _ = io.WriteString(w, okBody)
			}))
			t.Cleanup(srv.Close)

			if _, err := openweather.NewClient(srv.URL, "test-key", srv.Client()).Current(t.Context(), tt.city); err != nil {
				t.Fatalf("Current() err = %v", err)
			}
		})
	}
}

func TestClientCurrentTimeoutRedactsKey(t *testing.T) {
	const key = "s3cret key/+"
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	c := openweather.NewClient(srv.URL, key, &http.Client{Timeout: 50 * time.Millisecond})
	_, err := c.Current(t.Context(), "Москва")
	if !errors.Is(err, domain.ErrProviderUnavailable) {
		t.Fatalf("Current() err = %v, want ErrProviderUnavailable", err)
	}
	if strings.Contains(err.Error(), "s3cret") {
		t.Errorf("error leaks api key: %v", err)
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Errorf("error = %v, want redacted url", err)
	}
}

func TestClientCurrentCanceledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, okBody)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := openweather.NewClient(srv.URL, "test-key", srv.Client()).Current(ctx, "Москва")
	if !errors.Is(err, domain.ErrProviderUnavailable) {
		t.Errorf("Current() err = %v, want ErrProviderUnavailable", err)
	}
}

// roundTripFunc позволяет подменить транспорт без сети.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// countingBody считает байты, прочитанные клиентом из тела ответа.
type countingBody struct {
	r io.Reader
	n int64
}

func (b *countingBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	b.n += int64(n)
	return n, err
}

func (b *countingBody) Close() error { return nil }

func failingTransport() http.RoundTripper {
	return roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})
}

func TestClientCurrentTransportErrorRedactsEscapedKey(t *testing.T) {
	const key = "a+b/c"
	c := openweather.NewClient("http://example.test", key, &http.Client{Transport: failingTransport(), Timeout: time.Second})
	_, err := c.Current(t.Context(), "Москва")
	if !errors.Is(err, domain.ErrProviderUnavailable) {
		t.Fatalf("Current() err = %v, want ErrProviderUnavailable", err)
	}
	for _, leak := range []string{key, url.QueryEscape(key), url.PathEscape(key)} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("error leaks api key form %q: %v", leak, err)
		}
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Errorf("error = %v, want redacted url", err)
	}
}

func TestClientCurrentTransportErrorEmptyKey(t *testing.T) {
	c := openweather.NewClient("http://example.test", "", &http.Client{Transport: failingTransport(), Timeout: time.Second})
	_, err := c.Current(t.Context(), "Москва")
	if !errors.Is(err, domain.ErrProviderUnavailable) {
		t.Fatalf("Current() err = %v, want ErrProviderUnavailable", err)
	}
	if !strings.Contains(err.Error(), "appid=&") || strings.Contains(err.Error(), "[REDACTED]") {
		t.Errorf("error = %v, want it unchanged by redact", err)
	}
}

func TestClientCurrentBadBaseURL(t *testing.T) {
	_, err := openweather.NewClient("http://[::1", "test-key", http.DefaultClient).Current(t.Context(), "Москва")
	if err == nil || !strings.Contains(err.Error(), "parse base url") {
		t.Errorf("Current() err = %v, want parse base url error", err)
	}
}

func TestClientCurrentBuildRequestError(t *testing.T) {
	// nil ctx — единственный способ заставить http.NewRequestWithContext вернуть ошибку
	// при валидном URL; так покрывается ветка "build request".
	var ctx context.Context
	_, err := openweather.NewClient("http://example.test", "test-key", http.DefaultClient).Current(ctx, "Москва")
	if err == nil || !strings.Contains(err.Error(), "build request") {
		t.Errorf("Current() err = %v, want build request error", err)
	}
}

func TestClientCurrentBodyTooLarge(t *testing.T) {
	const limit = 1 << 20
	body := &countingBody{r: strings.NewReader(`{"name":"` + strings.Repeat("a", 2<<20) + `"}`)}
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body, Request: r}, nil
	})
	c := openweather.NewClient("http://example.test", "test-key", &http.Client{Transport: transport, Timeout: time.Second})
	_, err := c.Current(t.Context(), "Москва")
	if !errors.Is(err, domain.ErrProviderUnavailable) {
		t.Fatalf("Current() err = %v, want ErrProviderUnavailable", err)
	}
	if body.n > limit {
		t.Errorf("read %d bytes, want at most %d", body.n, limit)
	}
}

func TestClientCurrentEmptyWeather(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"name":"Москва","sys":{"country":"RU"},"weather":[]}`)
	}))
	t.Cleanup(srv.Close)

	got, err := openweather.NewClient(srv.URL, "test-key", srv.Client()).Current(t.Context(), "Москва")
	if err != nil {
		t.Fatalf("Current() err = %v", err)
	}
	if got.Description != "" || got.City != "Москва" {
		t.Errorf("Current() = %+v, want empty description", got)
	}
}
