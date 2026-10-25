package telegram

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"
)

type fakeUpdater struct {
	calls   int
	offsets []int64
	updates []Update
	err     error
}

func (f *fakeUpdater) GetUpdates(_ context.Context, offset int64) ([]Update, error) {
	f.calls++
	f.offsets = append(f.offsets, offset)
	return f.updates, f.err
}

type recordingHandler struct {
	messages []string
}

func (r *recordingHandler) Handle(_ context.Context, _ int64, text string) {
	r.messages = append(r.messages, text)
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestPollerProcessesUpdatesAndAdvancesOffset(t *testing.T) {
	fu := &fakeUpdater{updates: []Update{
		{UpdateID: 1, Message: &Message{Chat: Chat{ID: 10}, Text: "a"}},
		{UpdateID: 2, Message: &Message{Chat: Chat{ID: 10}, Text: "b"}},
		{UpdateID: 3}, // без сообщения
	}}
	rh := &recordingHandler{}
	p := NewPoller(fu, rh, quietLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	deadline := time.After(2 * time.Second)
	for len(rh.messages) < 2 {
		select {
		case <-deadline:
			t.Fatal("poller did not process updates in time")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() err = %v, want context.Canceled", err)
	}
	if fu.offsets[0] != 0 || fu.offsets[1] != 4 {
		t.Errorf("offsets = %v, want [0 4]", fu.offsets)
	}
	if len(rh.messages) != 2 || rh.messages[0] != "a" || rh.messages[1] != "b" {
		t.Errorf("messages = %v", rh.messages)
	}
}

func TestPollerBackoffOnErrors(t *testing.T) {
	fu := &fakeUpdater{err: errors.New("network down")}
	rh := &recordingHandler{}
	p := NewPoller(fu, rh, quietLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() err = %v, want DeadlineExceeded", err)
	}
	if fu.calls < 2 {
		t.Errorf("GetUpdates calls = %d, want >= 2 (backoff retries)", fu.calls)
	}
}

func TestPollerRateLimitWaitsRetryAfter(t *testing.T) {
	fu := &fakeUpdater{err: &rateLimitError{body: []byte(`{"ok":false,"parameters":{"retry_after":1}}`)}}
	rh := &recordingHandler{}
	p := NewPoller(fu, rh, quietLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	if err := p.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() err = %v, want DeadlineExceeded", err)
	}
	// 3 секунды: первый вызов + retry_after 1с + повтор + retry_after 1с + повтор ≈ 3 вызова.
	if fu.calls < 2 {
		t.Errorf("GetUpdates calls = %d, want >= 2", fu.calls)
	}
	if time.Since(start) < 2*time.Second {
		t.Errorf("poller retried too fast, retry_after not honored")
	}
}

func TestRetryAfterFromError(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		want   time.Duration
		wantOK bool
	}{
		{name: "with retry_after", err: &rateLimitError{body: []byte(`{"parameters":{"retry_after":5}}`)}, want: 5 * time.Second, wantOK: true},
		{name: "without parameters", err: &rateLimitError{body: []byte(`{"ok":false}`)}, want: backoffBase, wantOK: true},
		{name: "broken body", err: &rateLimitError{body: []byte(`not json`)}, want: backoffBase, wantOK: true},
		{name: "other error", err: errors.New("boom"), wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := retryAfterFromError(tt.err)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("retryAfterFromError() = (%v, %v), want (%v, %v)", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
