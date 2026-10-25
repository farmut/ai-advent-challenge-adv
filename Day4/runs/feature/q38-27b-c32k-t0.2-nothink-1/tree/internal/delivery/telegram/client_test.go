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

func TestClientGetUpdates(t *testing.T) {
	var gotOffset string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bot123:ABC/getUpdates" {
			t.Errorf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		gotOffset = q.Get("offset")
		if q.Get("timeout") != "30" {
			t.Errorf("timeout = %q", q.Get("timeout"))
		}
		if q.Get("allowed_updates") != `["message"]` {
			t.Errorf("allowed_updates = %q", q.Get("allowed_updates"))
		}
		_, _ = io.WriteString(w, `[{"update_id":7,"message":{"chat":{"id":42},"text":"/start"}}]`)
	}))
	t.Cleanup(srv.Close)

	c := telegram.NewClient(srv.URL, "123:ABC", srv.Client())
	updates, err := c.GetUpdates(context.Background(), 7)
	if err != nil {
		t.Fatalf("GetUpdates() err = %v", err)
	}
	if gotOffset != "7" {
		t.Errorf("offset = %q, want 7", gotOffset)
	}
	if len(updates) != 1 || updates[0].UpdateID != 7 || updates[0].Message.Text != "/start" {
		t.Errorf("updates = %+v", updates)
	}
}

func TestClientSend(t *testing.T) {
	var gotChatID, gotText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bot123:ABC/sendMessage" {
			t.Errorf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		gotChatID = q.Get("chat_id")
		gotText = q.Get("text")
		if q.Get("parse_mode") != "" {
			t.Errorf("parse_mode = %q, want empty", q.Get("parse_mode"))
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)

	c := telegram.NewClient(srv.URL, "123:ABC", srv.Client())
	if err := c.Send(context.Background(), 42, "Привет"); err != nil {
		t.Fatalf("Send() err = %v", err)
	}
	if gotChatID != "42" || gotText != "Привет" {
		t.Errorf("chat_id=%q text=%q", gotChatID, gotText)
	}
}

func TestClientSendRedactsToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"ok":false,"description":"Internal Server Error"}`)
	}))
	t.Cleanup(srv.Close)

	c := telegram.NewClient(srv.URL, "123:ABC", srv.Client())
	err := c.Send(context.Background(), 42, "Привет")
	if err == nil {
		t.Fatal("Send() err = nil, want error")
	}
	if strings.Contains(err.Error(), "123:ABC") {
		t.Errorf("err leaks token: %v", err)
	}
}

func TestClientGetUpdatesRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 3"}`)
	}))
	t.Cleanup(srv.Close)

	c := telegram.NewClient(srv.URL, "123:ABC", srv.Client())
	_, err := c.GetUpdates(context.Background(), 1)
	if err == nil {
		t.Fatal("GetUpdates() err = nil, want rate limit error")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("err = %v, want rate limited", err)
	}
}

func TestClientContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		_, _ = io.WriteString(w, `[]`)
	}))
	t.Cleanup(srv.Close)

	c := telegram.NewClient(srv.URL, "123:ABC", &http.Client{Timeout: 5 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.GetUpdates(ctx, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
}
