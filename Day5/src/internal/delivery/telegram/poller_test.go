package telegram

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
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
	ackErr  error
	acks    []ackCall
}

// ackCall — снимок вызова Ack: сколько GetUpdates было до него и в каком состоянии ctx.
type ackCall struct {
	offset    int64
	afterGets int
	ctxErr    error
	deadline  time.Duration // 0, если deadline нет
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

func (f *fakeUpdates) Ack(ctx context.Context, offset int64) error {
	c := ackCall{offset: offset, afterGets: len(f.offsets), ctxErr: ctx.Err()}
	if d, ok := ctx.Deadline(); ok {
		c.deadline = time.Until(d)
	}
	f.acks = append(f.acks, c)
	return f.ackErr
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
	if len(up.acks) != 0 { // offset 14 уже подтверждён успешным getUpdates
		t.Errorf("acks = %v, want none", up.acks)
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
	p, up, _, _, ctx := newTestPoller([]step{{updates: []Update{msg(5, 1, "Казань")}}, {err: ErrUnauthorized}})
	if err := p.Run(ctx); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("Run() err = %v, want ErrUnauthorized", err)
	}
	if len(up.acks) != 0 {
		t.Errorf("acks = %v, want none", up.acks)
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

// cancelHandler отменяет ctx после первого сообщения — как Ctrl+C сразу после ответа.
type cancelHandler struct {
	cancel context.CancelFunc
}

func (h cancelHandler) Handle(context.Context, int64, string) { h.cancel() }

func TestPollerAcksOffsetOnCancel(t *testing.T) {
	tests := []struct {
		name    string
		ackErr  error
		wantLog string
	}{
		{name: "ack ok"},
		{name: "ack fails", ackErr: errors.New("connection reset"), wantLog: "ack updates"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			up := &fakeUpdates{steps: []step{{updates: []Update{msg(100, 1, "Казань")}}}, cancel: cancel, ackErr: tt.ackErr}
			var logs bytes.Buffer
			p := NewPoller(up, cancelHandler{cancel}, slog.New(slog.NewTextHandler(&logs, nil)))

			if err := p.Run(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("Run() err = %v, want context.Canceled", err)
			}
			if len(up.acks) != 1 {
				t.Fatalf("acks = %v, want exactly one", up.acks)
			}
			a := up.acks[0]
			if a.offset != 101 || a.afterGets != len(up.offsets) {
				t.Errorf("ack = %+v after gets %v, want last call with offset 101", a, up.offsets)
			}
			if a.ctxErr != nil {
				t.Errorf("ack ctx err = %v, want live context", a.ctxErr)
			}
			if a.deadline <= 0 || a.deadline > ackTimeout {
				t.Errorf("ack deadline in %v, want (0, %v]", a.deadline, ackTimeout)
			}
			out := logs.String()
			if tt.wantLog != "" && (!strings.Contains(out, "level=WARN") || !strings.Contains(out, tt.wantLog)) {
				t.Errorf("log = %q, want WARN %q", out, tt.wantLog)
			}
		})
	}
}

func TestPollerNoAckWithoutUpdates(t *testing.T) {
	p, up, _, _, ctx := newTestPoller([]step{{updates: nil}})
	if err := p.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() err = %v, want context.Canceled", err)
	}
	if len(up.acks) != 0 {
		t.Errorf("acks = %v, want none", up.acks)
	}
}

func TestPollerAcksWhenCanceledDuringBackoff(t *testing.T) {
	p, up, _, _, ctx := newTestPoller([]step{
		{updates: []Update{msg(5, 1, "Казань")}},
		{err: errors.New("down")},
	})
	p.wait = func(context.Context, time.Duration) error {
		up.cancel()
		return context.Canceled
	}
	if err := p.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() err = %v, want context.Canceled", err)
	}
	if len(up.acks) != 1 || up.acks[0].offset != 6 {
		t.Errorf("acks = %+v, want one with offset 6", up.acks)
	}
}
