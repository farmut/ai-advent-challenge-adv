// Скрытый приёмочный тест для профиля bug-fixer. Агенту НЕ показывается:
// checks/bugfix.sh подкладывает его в копию итогового дерева как
// internal/delivery/telegram/zz_flood_acceptance_test.go.
//
// Опирается только на экспортированный API пакета telegram и на текст,
// видимый пользователю, поэтому не зависит от внутренних имён.
package telegram_test

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"weatherbot/internal/delivery/telegram"
	"weatherbot/internal/domain"
)

const accWarnMarker = "Не так быстро"

type accWeather struct{ calls atomic.Int32 }

func (w *accWeather) Current(_ context.Context, city string) (domain.Weather, error) {
	w.calls.Add(1)
	return domain.Weather{City: city, Country: "RU", TempC: 10, Description: "ясно"}, nil
}

type accSender struct {
	mu   sync.Mutex
	msgs map[int64][]string
}

func (s *accSender) Send(_ context.Context, chatID int64, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.msgs == nil {
		s.msgs = map[int64][]string{}
	}
	s.msgs[chatID] = append(s.msgs[chatID], text)
	return nil
}

func (s *accSender) chat(id int64) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.msgs[id]...)
}

func accWarnings(msgs []string) int {
	n := 0
	for _, m := range msgs {
		if strings.Contains(m, accWarnMarker) {
			n++
		}
	}
	return n
}

func accSetup() (*telegram.Handler, *accWeather, *accSender) {
	w, s := &accWeather{}, &accSender{}
	return telegram.NewHandler(w, s, slog.New(slog.DiscardHandler)), w, s
}

// accFlood: «Москва», затем сразу ещё 10 сообщений (≪ 2 с).
func accFlood(t *testing.T, h *telegram.Handler, chat int64) {
	t.Helper()
	h.Handle(t.Context(), chat, "Москва")
	for range 10 {
		h.Handle(t.Context(), chat, "Москва")
	}
}

func TestAcceptanceFloodSingleWarning(t *testing.T) {
	h, w, s := accSetup()
	accFlood(t, h, 1)
	got := s.chat(1)
	if c := w.calls.Load(); c != 1 {
		t.Errorf("weather calls = %d, want 1", c)
	}
	if len(got) == 0 || strings.Contains(got[0], accWarnMarker) {
		t.Errorf("first reply must be weather, got %q", got)
	}
	if n := accWarnings(got); n > 1 {
		t.Errorf("warnings in burst = %d, want <= 1", n)
	}
	if len(got) > 2 {
		t.Errorf("outgoing messages for burst of 11 = %d, want <= 2", len(got))
	}
}

func TestAcceptanceOtherChatNotBlocked(t *testing.T) {
	h, w, s := accSetup()
	accFlood(t, h, 1)
	h.Handle(t.Context(), 2, "Москва")
	got := s.chat(2)
	if len(got) != 1 || strings.Contains(got[0], accWarnMarker) {
		t.Errorf("chat 2 replies = %q, want exactly one weather reply", got)
	}
	if c := w.calls.Load(); c != 2 {
		t.Errorf("weather calls = %d, want 2", c)
	}
}

func TestAcceptanceRecoversAfterWindow(t *testing.T) {
	h, w, s := accSetup()
	accFlood(t, h, 1)
	before := len(s.chat(1))
	time.Sleep(2100 * time.Millisecond) // отсчёт от последнего сообщения всплеска
	h.Handle(t.Context(), 1, "Москва")
	got := s.chat(1)
	if len(got) != before+1 || strings.Contains(got[len(got)-1], accWarnMarker) {
		t.Errorf("after window: replies %q (before %d), want one new weather reply", got, before)
	}
	if c := w.calls.Load(); c != 2 {
		t.Errorf("weather calls = %d, want 2", c)
	}
}

// TestInfoSustainedFlood — информационный, в вердикт не входит (запускается отдельно).
func TestInfoSustainedFlood(t *testing.T) {
	h, w, s := accSetup()
	start := time.Now()
	n := 0
	for time.Since(start) < 4500*time.Millisecond {
		h.Handle(t.Context(), 1, "Москва")
		n++
		time.Sleep(200 * time.Millisecond)
	}
	got := s.chat(1)
	t.Logf("sustained: in=%d out=%d warnings=%d weather=%d", n, len(got), accWarnings(got), w.calls.Load())
	if accWarnings(got) > 3 || len(got) > 6 {
		t.Errorf("sustained flood: out=%d warnings=%d, want out<=6 warnings<=3", len(got), accWarnings(got))
	}
}
