package weather

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const sampleResponse = `{
  "name": "Moscow",
  "sys": {"country": "RU"},
  "weather": [{"description": "облачно"}],
  "main": {"temp": 5.3, "feels_like": 2.1, "humidity": 80, "pressure": 1012},
  "wind": {"speed": 3.5}
}`

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewClient(srv.Client(), srv.URL, "test-key", "metric", "ru")
}

func TestCurrent_Success(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/weather" {
			t.Errorf("path = %q, want /weather", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("q") != "Нижний Новгород" || q.Get("appid") != "test-key" || q.Get("units") != "metric" || q.Get("lang") != "ru" {
			t.Errorf("unexpected query: %v", q)
		}
		_, _ = w.Write([]byte(sampleResponse))
	})

	got, err := client.Current(context.Background(), "  Нижний Новгород ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := Weather{City: "Moscow", Country: "RU", Description: "облачно", Temp: 5.3, FeelsLike: 2.1, Humidity: 80, Pressure: 1012, WindSpeed: 3.5}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestCurrent_StatusErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{"not found", http.StatusNotFound, `{"cod":"404","message":"city not found"}`, ErrCityNotFound},
		{"unauthorized", http.StatusUnauthorized, `{"cod":401,"message":"Invalid API key"}`, ErrUnauthorized},
		{"rate limited", http.StatusTooManyRequests, `{"cod":429,"message":"limit"}`, ErrRateLimited},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})
			_, err := client.Current(context.Background(), "X")
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestCurrent_UnexpectedStatus(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
	})
	_, err := client.Current(context.Background(), "X")
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %v, want status 500 error", err)
	}
}

func TestCurrent_InvalidJSON(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	})
	if _, err := client.Current(context.Background(), "X"); err == nil {
		t.Error("expected decode error")
	}
}

func TestCurrent_EmptyCity(t *testing.T) {
	client := newTestClient(t, func(http.ResponseWriter, *http.Request) {
		t.Error("server must not be called for empty city")
	})
	if _, err := client.Current(context.Background(), "   "); !errors.Is(err, ErrCityNotFound) {
		t.Errorf("err = %v, want ErrCityNotFound", err)
	}
}

func TestCurrent_TransportErrorDoesNotLeakKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close() // закрытый сервер — гарантированная ошибка соединения без выхода в сеть

	client := NewClient(srv.Client(), srv.URL, "super-secret", "metric", "ru")
	_, err := client.Current(context.Background(), "X")
	if err == nil {
		t.Fatal("expected transport error")
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Errorf("API key leaked in error: %v", err)
	}
}
