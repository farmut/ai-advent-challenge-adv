package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
)

// DefaultReply is the fake upstream answer when a case scripts none.
const DefaultReply = "Рекомендую провести ретроспективу в формате Start/Stop/Continue и ограничить её 60 минутами."

// FakeUpstream is a scripted OpenAI-compatible server.
type FakeUpstream struct {
	*httptest.Server
	mu    sync.Mutex
	reply string
	usage *Usage
}

// NewFakeUpstream starts the server.
func NewFakeUpstream() *FakeUpstream {
	f := &FakeUpstream{reply: DefaultReply, usage: &Usage{Prompt: 40, Completion: 20}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.handle))
	return f
}

// Script sets the next replies; an empty reply uses DefaultReply and a nil
// usage omits the usage object.
func (f *FakeUpstream) Script(reply string, usage *Usage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if reply == "" {
		reply = DefaultReply
	}
	f.reply, f.usage = reply, usage
}

func (f *FakeUpstream) handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	_, _ = io.Copy(io.Discard, r.Body)
	f.mu.Lock()
	reply, usage := f.reply, f.usage
	f.mu.Unlock()
	resp := map[string]any{
		"id": "fake", "object": "chat.completion",
		"choices": []map[string]any{{"index": 0, "message": map[string]string{"role": "assistant", "content": reply}, "finish_reason": "stop"}},
	}
	if usage != nil {
		resp["usage"] = map[string]int{"prompt_tokens": usage.Prompt, "completion_tokens": usage.Completion, "total_tokens": usage.Prompt + usage.Completion}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
