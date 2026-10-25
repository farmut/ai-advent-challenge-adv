package openweather_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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
