package telegram

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type step struct {
	updates []Update
	err     error
}

// fakeUpdates отдаёт заранее заданные ответы, затем отменяет контекст.
type fakeUpdates struct {
	steps   []step
	offsets []int64
	cancel  context.CancelFunc
}

func (f *fakeUpdates) GetUpdates(_ context.Context, offset int64) ([]Update, error) {
	f.offsets = append(f.offsets, offset)
	if len(f.offsets) > len(f.steps) {
		f.cancel()
		return nil, context.Canceled
	}
	s := f.steps[len(f.offsets)-1]
	return s.updates, s.err
}

type handled struct {
	chatID int64
	text   string
}

type fakeHandler struct {
	mu  sync.Mutex
	got []handled
}

func (f *fakeHandler) Handle(_ context.Context, chatID int64, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, handled{chatID, text})
}

func newTestPoller(steps []step) (*Poller, *fakeUpdates, *fakeHandler, *[]time.Duration, context.Context) {
	ctx, cancel := context.WithCancel(context.Background())
	up := &fakeUpdates{steps: steps, cancel: cancel}
	h := &fakeHandler{}
	p := NewPoller(up, h, slog.New(slog.DiscardHandler))
	var waits []time.Duration
	p.wait = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		return ctx.Err()
	}
	return p, up, h, &waits, ctx
}

func msg(id, chatID int64, text string) Update {
	return Update{UpdateID: id, Message: &Message{Chat: Chat{ID: chatID}, Text: text}}
}

func TestPollerDispatchesAndAdvancesOffset(t *testing.T) {
	p, up, h, _, ctx := newTestPoller([]step{
		{updates: []Update{msg(10, 1, "Казань"), {UpdateID: 11}, msg(12, 2, "")}},
		{updates: []Update{msg(13, 2, "/start")}},
		{updates: nil},
	})

	if err := p.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() err = %v, want context.Canceled", err)
	}
	wantOffsets := []int64{0, 13, 14, 14}
	if len(up.offsets) != len(wantOffsets) {
		t.Fatalf("offsets = %v, want %v", up.offsets, wantOffsets)
	}
	for i := range wantOffsets {
		if up.offsets[i] != wantOffsets[i] {
			t.Errorf("offsets = %v, want %v", up.offsets, wantOffsets)
			break
		}
	}
	want := []handled{{1, "Казань"}, {2, "/start"}}
	if len(h.got) != len(want) || h.got[0] != want[0] || h.got[1] != want[1] {
		t.Errorf("handled = %v, want %v", h.got, want)
	}
}

func TestPollerBackoffAndRetryAfter(t *testing.T) {
	transient := errors.New("connection reset")
	p, _, _, waits, ctx := newTestPoller([]step{
		{err: transient},
		{err: transient},
		{err: &retryAfterError{after: 7 * time.Second}},
		{err: transient},
		{updates: []Update{}},
		{err: transient},
	})

	if err := p.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() err = %v, want context.Canceled", err)
	}
	want := []time.Duration{time.Second, 2 * time.Second, 7 * time.Second, 4 * time.Second, time.Second}
	if len(*waits) != len(want) {
		t.Fatalf("waits = %v, want %v", *waits, want)
	}
	for i := range want {
		if (*waits)[i] != want[i] {
			t.Errorf("waits = %v, want %v", *waits, want)
			break
		}
	}
}

func TestPollerBackoffIsCapped(t *testing.T) {
	steps := make([]step, 10)
	for i := range steps {
		steps[i] = step{err: errors.New("down")}
	}
	p, _, _, waits, ctx := newTestPoller(steps)
	_ = p.Run(ctx)
	if last := (*waits)[len(*waits)-1]; last != maxBackoff {
		t.Errorf("last wait = %v, want %v", last, maxBackoff)
	}
}

func TestPollerStopsOnUnauthorized(t *testing.T) {
	p, _, _, _, ctx := newTestPoller([]step{{err: ErrUnauthorized}})
	if err := p.Run(ctx); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("Run() err = %v, want ErrUnauthorized", err)
	}
}

func TestPollerStopsWhileWaiting(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	up := &fakeUpdates{steps: []step{{err: errors.New("down")}}, cancel: cancel}
	p := NewPoller(up, &fakeHandler{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	time.AfterFunc(20*time.Millisecond, cancel) // реальный sleep на 1 с должен прерваться

	start := time.Now()
	if err := p.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() err = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("Run() took %v, want prompt stop on cancel", elapsed)
	}
}
