package telegram_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"weatherbot/internal/delivery/telegram"
)

func TestClientSend(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/bottest-token/sendMessage" {
				t.Errorf("expected /bottest-token/sendMessage, got %s", r.URL.Path)
			}
			q := r.URL.Query()
			if q.Get("chat_id") != "123456789" {
				t.Errorf("expected chat_id=123456789, got %s", q.Get("chat_id"))
			}
			if q.Get("text") != "Привет!" {
				t.Errorf("expected text=Привет!, got %s", q.Get("text"))
			}
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":123}}`)
		}))
		t.Cleanup(srv.Close)

		c := telegram.NewClient(srv.URL, "test-token", srv.Client())
		err := c.Send(context.Background(), 123456789, "Привет!")
		if err != nil {
			t.Fatalf("Send() error = %v", err)
		}
	})

	t.Run("error status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"ok":false,"error_code":500,"description":"Internal Server Error"}`)
		}))
		t.Cleanup(srv.Close)

		c := telegram.NewClient(srv.URL, "test-token", srv.Client())
		err := c.Send(context.Background(), 123456789, "Привет!")
		if err == nil {
			t.Fatalf("Send() error = nil, want error")
		}
	})
}

func TestClientGetUpdates(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/bottest-token/getUpdates" {
				t.Errorf("expected /bottest-token/getUpdates, got %s", r.URL.Path)
			}
			q := r.URL.Query()
			if q.Get("offset") != "123" {
				t.Errorf("expected offset=123, got %s", q.Get("offset"))
			}
			if q.Get("timeout") != "30" {
				t.Errorf("expected timeout=30, got %s", q.Get("timeout"))
			}
			if q.Get("allowed_updates") != `["message"]` {
				t.Errorf("expected allowed_updates=[\"message\"], got %s", q.Get("allowed_updates"))
			}
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"ok":true,"result":[{"update_id":124,"message":{"message_id":123,"chat":{"id":123456789},"text":"Привет!"}}]}`)
		}))
		t.Cleanup(srv.Close)

		c := telegram.NewClient(srv.URL, "test-token", srv.Client())
		updates, err := c.GetUpdates(context.Background(), 123)
		if err != nil {
			t.Fatalf("GetUpdates() error = %v", err)
		}
		if len(updates) != 1 {
			t.Fatalf("GetUpdates() len = %d, want 1", len(updates))
		}
		if updates[0].ID != 124 {
			t.Errorf("GetUpdates() ID = %d, want 124", updates[0].ID)
		}
		if updates[0].Message.Text != "Привет!" {
			t.Errorf("GetUpdates() Text = %s, want Привет!", updates[0].Message.Text)
		}
	})

	t.Run("rate limited", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 30","parameters":{"retry_after":30}}`)
		}))
		t.Cleanup(srv.Close)

		c := telegram.NewClient(srv.URL, "test-token", srv.Client())
		_, err := c.GetUpdates(context.Background(), 123)
		if err == nil {
			t.Fatalf("GetUpdates() error = nil, want error")
		}
		if !strings.Contains(err.Error(), "retry after") {
			t.Errorf("GetUpdates() error = %v, want to contain 'retry after'", err)
		}
	})

	t.Run("error status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"ok":false,"error_code":500,"description":"Internal Server Error"}`)
		}))
		t.Cleanup(srv.Close)

		c := telegram.NewClient(srv.URL, "test-token", srv.Client())
		_, err := c.GetUpdates(context.Background(), 123)
		if err == nil {
			t.Fatalf("GetUpdates() error = nil, want error")
		}
	})
}