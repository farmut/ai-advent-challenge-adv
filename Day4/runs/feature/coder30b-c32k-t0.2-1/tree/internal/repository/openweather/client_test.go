package openweather_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"weatherbot/internal/domain"
	"weatherbot/internal/repository/openweather"
)

const okBody = `{"name":"Москва","sys":{"country":"RU"},"main":{"temp":12.4,"feels_like":9.6,"humidity":71},
"weather":[{"description":"переменная облачность"}],"wind":{"speed":4.2,"deg":315}}`

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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				if r.URL.Path != "/data/2.5/weather" || q.Get("q") != "Москва" || q.Get("appid") != "test-key" {
					t.Errorf("unexpected request: %s", r.URL)
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