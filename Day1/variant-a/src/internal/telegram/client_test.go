package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewClient(srv.Client(), srv.URL, "TOKEN")
}

func decodeBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	return m
}

func TestGetUpdates(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botTOKEN/getUpdates" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		body := decodeBody(t, r)
		if body["offset"] != float64(42) || body["timeout"] != float64(30) {
			t.Errorf("unexpected params: %v", body)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":42,"message":{"message_id":1,"chat":{"id":7},"text":"Москва"}}]}`))
	})

	updates, err := client.GetUpdates(context.Background(), 42, 30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updates) != 1 || updates[0].UpdateID != 42 || updates[0].Message.Chat.ID != 7 || updates[0].Message.Text != "Москва" {
		t.Errorf("unexpected updates: %+v", updates)
	}
}

func TestSendMessage(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botTOKEN/sendMessage" {
			t.Errorf("path = %q", r.URL.Path)
		}
		body := decodeBody(t, r)
		if body["chat_id"] != float64(7) || body["text"] != "привет" {
			t.Errorf("unexpected params: %v", body)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	})

	if err := client.SendMessage(context.Background(), 7, "привет"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCall_APIError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":401,"description":"Unauthorized"}`))
	})

	err := client.SendMessage(context.Background(), 1, "x")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 401 {
		t.Fatalf("err = %v, want APIError 401", err)
	}
}

func TestCall_InvalidJSON(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>"))
	})
	if _, err := client.GetUpdates(context.Background(), 0, 0); err == nil {
		t.Error("expected decode error")
	}
}

func TestCall_TransportErrorDoesNotLeakToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()

	client := NewClient(srv.Client(), srv.URL, "super-secret-token")
	err := client.SendMessage(context.Background(), 1, "x")
	if err == nil {
		t.Fatal("expected transport error")
	}
	if strings.Contains(err.Error(), "super-secret-token") {
		t.Errorf("token leaked in error: %v", err)
	}
}
