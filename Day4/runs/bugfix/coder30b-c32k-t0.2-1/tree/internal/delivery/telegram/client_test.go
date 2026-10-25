package telegram

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testToken = "123456:test-token"

func TestClientGetUpdates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.Method != http.MethodGet || r.URL.Path != "/bot"+testToken+"/getUpdates" ||
			q.Get("offset") != "42" || q.Get("timeout") != "30" || q.Get("allowed_updates") != `["message"]` {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":[
			{"update_id":42,"message":{"chat":{"id":7},"text":"Казань"}},
			{"update_id":43,"edited_message":{"chat":{"id":7},"text":"x"}}]}`)
	}))
	t.Cleanup(srv.Close)

	got, err := NewClient(srv.URL, testToken, srv.Client()).GetUpdates(t.Context(), 42)
	if err != nil {
		t.Fatalf("GetUpdates() err = %v", err)
	}
	if len(got) != 2 || got[0].UpdateID != 42 || got[0].Message == nil ||
		got[0].Message.Chat.ID != 7 || got[0].Message.Text != "Казань" || got[1].Message != nil {
		t.Errorf("GetUpdates() = %+v", got)
	}
}

func TestClientSend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/bot"+testToken+"/sendMessage" ||
			r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body["chat_id"] != float64(7) || body["text"] != "<b>hi</b>" {
			t.Errorf("unexpected body: %v", body)
		}
		if _, ok := body["parse_mode"]; ok {
			t.Error("parse_mode must not be set")
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":{}}`)
	}))
	t.Cleanup(srv.Close)

	if err := NewClient(srv.URL, testToken, srv.Client()).Send(t.Context(), 7, "<b>hi</b>"); err != nil {
		t.Fatalf("Send() err = %v", err)
	}
}

func TestClientErrors(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		check     func(error) bool
		wantInErr string
	}{
		{
			name: "retry after", status: http.StatusTooManyRequests,
			body: `{"ok":false,"error_code":429,"parameters":{"retry_after":5}}`,
			check: func(err error) bool {
				var ra *retryAfterError
				return errors.As(err, &ra) && ra.after == 5*time.Second
			},
		},
		{
			name: "unauthorized", status: http.StatusUnauthorized, body: `{"ok":false}`,
			check: func(err error) bool { return errors.Is(err, ErrUnauthorized) },
		},
		{
			name: "bad request", status: http.StatusBadRequest, body: `{"ok":false,"description":"Bad Request: chat not found"}`,
			check: func(err error) bool { return err != nil }, wantInErr: "chat not found",
		},
		{
			name: "server error", status: http.StatusBadGateway, body: `<html>`,
			check: func(err error) bool { return err != nil }, wantInErr: "status 502",
		},
		{
			name: "ok false", status: http.StatusOK, body: `{"ok":false,"description":"weird"}`,
			check: func(err error) bool { return err != nil }, wantInErr: "weird",
		},
		{
			name: "broken json", status: http.StatusOK, body: `{"ok":`,
			check: func(err error) bool { return err != nil }, wantInErr: "decode",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			t.Cleanup(srv.Close)

			_, err := NewClient(srv.URL, testToken, srv.Client()).GetUpdates(t.Context(), 0)
			if !tt.check(err) {
				t.Fatalf("GetUpdates() unexpected err = %v", err)
			}
			if !strings.Contains(err.Error(), tt.wantInErr) {
				t.Errorf("GetUpdates() err = %q, want it to contain %q", err, tt.wantInErr)
			}
		})
	}
}

func TestClientRedactsTokenFromTransportErrors(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-release // держим запрос дольше клиентского таймаута
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) }) // выполнится раньше srv.Close

	c := NewClient(srv.URL, testToken, &http.Client{Timeout: 50 * time.Millisecond})
	errs := []error{c.Send(t.Context(), 1, "hi")}
	_, err := c.GetUpdates(t.Context(), 0)
	errs = append(errs, err)
	for _, err := range errs {
		if err == nil {
			t.Fatal("want timeout error, got nil")
		}
		if strings.Contains(err.Error(), "test-token") || strings.Contains(err.Error(), "123456") {
			t.Errorf("error leaks token: %v", err)
		}
		if !strings.Contains(err.Error(), "[REDACTED]") {
			t.Errorf("error = %v, want redacted url", err)
		}
	}
}
