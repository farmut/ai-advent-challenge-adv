package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"weatherbot/internal/domain"
)

type fakeWeather struct {
	w       domain.Weather
	err     error
	panics  bool
	gotCity string
}

func (f *fakeWeather) Current(_ context.Context, city string) (domain.Weather, error) {
	if f.panics {
		panic("boom")
	}
	f.gotCity = city
	return f.w, f.err
}

type fakeSender struct {
	sent []string
	err  error
}

func (f *fakeSender) Send(_ context.Context, _ int64, text string) error {
	f.sent = append(f.sent, text)
	return f.err
}

func newTestHandler(w *fakeWeather) (*Handler, *fakeSender) {
	s := &fakeSender{}
	return NewHandler(w, s, slog.New(slog.DiscardHandler)), s
}

func TestHandlerResponses(t *testing.T) {
	moscow := domain.Weather{City: "Москва", Country: "RU", TempC: 12.4, FeelsLikeC: 9.6, Description: "ясно", Humidity: 71, WindSpeed: 4.2, WindDeg: 315}
	tests := []struct {
		name     string
		text     string
		weather  fakeWeather
		want     string
		wantCity string
	}{
		{name: "start", text: "/start", want: msgStart},
		{name: "help", text: "/help", want: msgHelp},
		{name: "unknown command", text: "/foo bar", want: msgHelp},
		{name: "weather without city", text: "/weather", want: msgEmptyCity},
		{name: "weather spaces only", text: "/weather    ", want: msgEmptyCity},
		{name: "weather", text: "/weather Москва", weather: fakeWeather{w: moscow}, want: formatWeather(moscow), wantCity: "Москва"},
		{name: "weather with bot mention", text: "/weather@MyBot Казань", weather: fakeWeather{w: moscow}, want: formatWeather(moscow), wantCity: "Казань"},
		{name: "uppercase command", text: "/WEATHER Казань", weather: fakeWeather{w: moscow}, want: formatWeather(moscow), wantCity: "Казань"},
		{name: "newline separator", text: "/weather\nКазань", weather: fakeWeather{w: moscow}, want: formatWeather(moscow), wantCity: "Казань"},
		{name: "plain text city", text: "  Москва ", weather: fakeWeather{w: moscow}, want: formatWeather(moscow), wantCity: "Москва"},
		{name: "invalid city", text: "123", weather: fakeWeather{err: domain.ErrInvalidCity}, want: msgInvalidCity, wantCity: "123"},
		{
			name: "not found", text: "/weather  Нигде   Бург", weather: fakeWeather{err: fmt.Errorf("get current weather: %w", domain.ErrCityNotFound)},
			want: msgCityNotFound("Нигде Бург"), wantCity: "Нигде   Бург",
		},
		{name: "unavailable", text: "Москва", weather: fakeWeather{err: domain.ErrProviderUnavailable}, want: msgUnavailable, wantCity: "Москва"},
		{name: "auth", text: "Москва", weather: fakeWeather{err: domain.ErrProviderAuth}, want: msgUnavailable, wantCity: "Москва"},
		{name: "rate limited", text: "Москва", weather: fakeWeather{err: domain.ErrRateLimited}, want: msgUnavailable, wantCity: "Москва"},
		{name: "unknown error", text: "Москва", weather: fakeWeather{err: errors.New("secret internals")}, want: msgUnavailable, wantCity: "Москва"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := tt.weather
			h, s := newTestHandler(&w)
			h.Handle(t.Context(), 1, tt.text)
			if len(s.sent) != 1 || s.sent[0] != tt.want {
				t.Errorf("sent = %q, want [%q]", s.sent, tt.want)
			}
			if w.gotCity != tt.wantCity {
				t.Errorf("weather got city %q, want %q", w.gotCity, tt.wantCity)
			}
			if len(s.sent) == 1 && strings.Contains(s.sent[0], "secret") {
				t.Errorf("internal error leaked to user: %q", s.sent[0])
			}
		})
	}
}

func TestHandlerRateLimit(t *testing.T) {
	w := &fakeWeather{w: domain.Weather{City: "Москва"}}
	h, s := newTestHandler(w)

	h.Handle(t.Context(), 1, "/start")
	h.Handle(t.Context(), 1, "Москва")
	h.Handle(t.Context(), 2, "/start")

	want := []string{msgStart, msgTooFrequent, msgStart}
	if strings.Join(s.sent, "|") != strings.Join(want, "|") {
		t.Errorf("sent = %q, want %q", s.sent, want)
	}
	if w.gotCity != "" {
		t.Errorf("weather must not be called when rate limited, got city %q", w.gotCity)
	}
}

func TestHandlerAllow(t *testing.T) {
	h, _ := newTestHandler(&fakeWeather{})
	now := time.Now()
	steps := []struct {
		at   time.Duration
		want bool
	}{{0, true}, {time.Second, false}, {1999 * time.Millisecond, false}, {2 * time.Second, true}, {3 * time.Second, false}, {4 * time.Second, true}}
	for _, st := range steps {
		if got := h.allow(1, now.Add(st.at)); got != st.want {
			t.Errorf("allow(+%v) = %v, want %v", st.at, got, st.want)
		}
	}
}

func TestHandlerAllowEvictsStaleChats(t *testing.T) {
	h, _ := newTestHandler(&fakeWeather{})
	now := time.Now()
	for id := range int64(maxTrackedChats) {
		h.allow(id, now)
	}
	h.allow(-1, now.Add(time.Minute))
	if len(h.lastReq) != 1 {
		t.Errorf("tracked chats = %d, want 1 after eviction", len(h.lastReq))
	}
}

func TestHandlerRecoversFromPanic(t *testing.T) {
	h, s := newTestHandler(&fakeWeather{panics: true})
	h.Handle(t.Context(), 1, "Москва") // не должно паниковать
	if len(s.sent) != 0 {
		t.Errorf("sent = %q, want nothing", s.sent)
	}
	h.Handle(t.Context(), 2, "/start")
	if len(s.sent) != 1 || s.sent[0] != msgStart {
		t.Errorf("handler broken after panic, sent = %q", s.sent)
	}
}

func TestHandlerSendErrorIsLoggedNotPanicked(t *testing.T) {
	w := &fakeWeather{}
	s := &fakeSender{err: errors.New("network down")}
	h := NewHandler(w, s, slog.New(slog.DiscardHandler))
	h.Handle(t.Context(), 1, "/start")
	if len(s.sent) != 1 {
		t.Errorf("send attempts = %d, want 1", len(s.sent))
	}
}

func TestParseCommand(t *testing.T) {
	tests := []struct{ in, cmd, arg string }{
		{"/weather@MyBot Казань", "/weather", "Казань"},
		{"/weather   Нижний Новгород ", "/weather", "Нижний Новгород"},
		{"/start", "/start", ""},
		{"/Help@MyBot", "/help", ""},
		{"Казань", "", "Казань"},
		{"  ", "", ""},
	}
	for _, tt := range tests {
		cmd, arg := parseCommand(tt.in)
		if cmd != tt.cmd || arg != tt.arg {
			t.Errorf("parseCommand(%q) = (%q, %q), want (%q, %q)", tt.in, cmd, arg, tt.cmd, tt.arg)
		}
	}
}
