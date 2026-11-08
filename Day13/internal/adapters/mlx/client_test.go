package mlx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agile-consultant/internal/config"
	"agile-consultant/internal/ports"
)

func TestGenerateSendsRolesVerbatim(t *testing.T) {
	var got chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, part := range []string{"Вердикт: ", "ok"} {
			chunk, _ := json.Marshal(streamResponse{Choices: []streamChoice{{Delta: delta{Content: part}}}})
			fmt.Fprintf(w, "data: %s\n\n", chunk)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := NewClient(srv.URL, "/models/m", &config.Inference{MaxTokens: 16, Temperature: 0, TopP: 1}, logger)
	msgs := []ports.Message{
		{Role: ports.RoleSystem, Content: "rules"},
		{Role: ports.RoleUser, Content: "<user_question>\nq\n</user_question>"},
		{Role: ports.RoleSystem, Content: "reminder"},
	}
	resp, err := c.Generate(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Raw != "Вердикт: ok" {
		t.Errorf("raw = %q", resp.Raw)
	}
	if len(got.Messages) != len(msgs) {
		t.Fatalf("messages = %+v", got.Messages)
	}
	for i, m := range msgs {
		if got.Messages[i].Role != m.Role || got.Messages[i].Content != m.Content {
			t.Errorf("message %d = %+v, want %+v", i, got.Messages[i], m)
		}
		if strings.Contains(got.Messages[i].Content, "<|") {
			t.Errorf("message %d contains ChatML special token", i)
		}
	}
	if got.Model != "/models/m" || got.MaxTokens != 16 || !got.Stream {
		t.Errorf("request = %+v", got)
	}
}
