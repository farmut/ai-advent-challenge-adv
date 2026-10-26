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
	calls   int
}

func (f *fakeWeather) Current(_ context.Context, city string) (domain.Weather, error) {
	if f.panics {
		panic("boom")
	}
	f.gotCity = city
	f.calls++
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

const testBotUsername = "MyBot"

func newTestHandler(w *fakeWeather) (*Handler, *fakeSender) {
	s := &fakeSender{}
	return NewHandler(w, s, testBotUsername, slog.New(slog.DiscardHandler)), s
}

// newClockedTestHandler — newTestHandler с лимитером на фейковых часах.
func newClockedTestHandler(w *fakeWeather) (*Handler, *fakeClock) {
	h, _ := newTestHandler(w)
	c := newFakeClock()
	h.limiter = newChatLimiter(perChatInterval, maxTrackedChats, c.Now)
	return h, c
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
			name: "not found", text: "  Нью   Йорк  ",
			weather: fakeWeather{err: fmt.Errorf("get current weather: %w", &domain.CityNotFoundError{City: "Нью Йорк"})},
			want:    "🤔 Не нашёл город «Нью Йорк». Проверь написание или попробуй по-английски", wantCity: "Нью   Йорк",
		},
		{
			name: "not found bare sentinel", text: "/weather  Нигде   Бург", weather: fakeWeather{err: domain.ErrCityNotFound},
			want: msgCityNotFound("Нигде   Бург"), wantCity: "Нигде   Бург",
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

func TestHandlerBotMention(t *testing.T) {
	kazan := domain.Weather{City: "Казань", Country: "RU"}
	tests := []struct {
		name     string
		text     string
		want     []string
		wantCity string
	}{
		{name: "own bot, other case", text: "/weather@mybot Казань", want: []string{formatWeather(kazan)}, wantCity: "Казань"},
		{name: "own bot help", text: "/help@MYBOT", want: []string{msgHelp}},
		{name: "empty mention is ours", text: "/help@", want: []string{msgHelp}},
		{name: "other bot", text: "/weather@OtherBot Казань"},
		{name: "other bot help", text: "/help@OtherBot"},
		{name: "no mention", text: "/weather Казань", want: []string{formatWeather(kazan)}, wantCity: "Казань"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &fakeWeather{w: kazan}
			h, s := newTestHandler(w)
			h.Handle(t.Context(), 1, tt.text)
			if strings.Join(s.sent, "|") != strings.Join(tt.want, "|") {
				t.Errorf("sent = %q, want %q", s.sent, tt.want)
			}
			if w.gotCity != tt.wantCity {
				t.Errorf("weather got city %q, want %q", w.gotCity, tt.wantCity)
			}
			if len(tt.want) == 0 { // чужая команда не должна тратить rate limit
				s.sent = nil
				h.Handle(t.Context(), 1, "Казань")
				if len(s.sent) != 1 || s.sent[0] != formatWeather(kazan) {
					t.Errorf("after ignored command sent = %q, want [%q]", s.sent, formatWeather(kazan))
				}
				if w.calls != 1 {
					t.Errorf("weather calls = %d, want 1", w.calls)
				}
			}
		})
	}
}

func TestHandlerRateLimit(t *testing.T) {
	moscow := domain.Weather{City: "Москва"}
	w := &fakeWeather{w: moscow}
	h, s := newTestHandler(w)

	h.Handle(t.Context(), 1, "Москва")
	h.Handle(t.Context(), 1, "Москва")
	h.Handle(t.Context(), 2, "/start")

	want := []string{formatWeather(moscow), msgTooFrequent, msgStart}
	if strings.Join(s.sent, "|") != strings.Join(want, "|") {
		t.Errorf("sent = %q, want %q", s.sent, want)
	}
	if w.calls != 1 {
		t.Errorf("weather calls = %d, want 1: rate limited request must not reach provider", w.calls)
	}
}

// Лимит тратит только поход к провайдеру погоды. Невалидный город тоже тратит: валидация в usecase.
func TestHandlerRateLimitOnlyWeather(t *testing.T) {
	kazan := domain.Weather{City: "Казань", Country: "RU"}
	tests := []struct {
		name      string
		first     string
		firstErr  error
		want      []string
		wantCalls int
	}{
		{name: "start", first: "/start", want: []string{msgStart, formatWeather(kazan)}, wantCalls: 1},
		{name: "help", first: "/help", want: []string{msgHelp, formatWeather(kazan)}, wantCalls: 1},
		{name: "unknown command", first: "/foo", want: []string{msgHelp, formatWeather(kazan)}, wantCalls: 1},
		{name: "weather without city", first: "/weather", want: []string{msgEmptyCity, formatWeather(kazan)}, wantCalls: 1},
		{name: "invalid city", first: "123", firstErr: domain.ErrInvalidCity, want: []string{msgInvalidCity, msgTooFrequent}, wantCalls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &fakeWeather{err: tt.firstErr}
			h, s := newTestHandler(w)
			h.Handle(t.Context(), 1, tt.first)
			w.w, w.err = kazan, nil
			h.Handle(t.Context(), 1, "Казань")
			if strings.Join(s.sent, "|") != strings.Join(tt.want, "|") {
				t.Errorf("sent = %q, want %q", s.sent, tt.want)
			}
			if w.calls != tt.wantCalls {
				t.Errorf("weather calls = %d, want %d", w.calls, tt.wantCalls)
			}
		})
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
	h := NewHandler(w, s, testBotUsername, slog.New(slog.DiscardHandler))
	h.Handle(t.Context(), 1, "/start")
	if len(s.sent) != 1 {
		t.Errorf("send attempts = %d, want 1", len(s.sent))
	}
}

func TestParseCommand(t *testing.T) {
	tests := []struct{ in, cmd, mention, arg string }{
		{"/weather@MyBot Казань", "/weather", "MyBot", "Казань"},
		{"/weather   Нижний Новгород ", "/weather", "", "Нижний Новгород"},
		{"/start", "/start", "", ""},
		{"/Help@MyBot", "/help", "MyBot", ""},
		{"/help@", "/help", "", ""},
		{"/weather Казань@x", "/weather", "", "Казань@x"},
		{"Казань@OtherBot", "", "", "Казань@OtherBot"},
		{"/a@b@c", "/a", "b@c", ""},
		{"Казань", "", "", "Казань"},
		{"  ", "", "", ""},
	}
	for _, tt := range tests {
		cmd, mention, arg := parseCommand(tt.in)
		if cmd != tt.cmd || mention != tt.mention || arg != tt.arg {
			t.Errorf("parseCommand(%q) = (%q, %q, %q), want (%q, %q, %q)", tt.in, cmd, mention, arg, tt.cmd, tt.mention, tt.arg)
		}
	}
}

func TestHandlerRepeatsLastCity(t *testing.T) {
	kazan := domain.Weather{City: "Казань", Country: "RU"}
	w := &fakeWeather{w: kazan}
	h, c := newClockedTestHandler(w)

	if got := h.respond(t.Context(), 1, "", "Казань"); got != formatWeather(kazan) {
		t.Fatalf("first reply = %q, want weather", got)
	}
	w.gotCity = ""
	c.Advance(3 * time.Second)
	if got := h.respond(t.Context(), 1, "/weather", ""); got != formatWeather(kazan) {
		t.Errorf("repeat reply = %q, want weather", got)
	}
	if w.gotCity != "Казань" {
		t.Errorf("weather got city %q, want %q", w.gotCity, "Казань")
	}
	if got := h.respond(t.Context(), 2, "/weather", ""); got != msgEmptyCity {
		t.Errorf("other chat reply = %q, want %q", got, msgEmptyCity)
	}
	if w.calls != 2 {
		t.Errorf("weather calls = %d, want 2: chat without history must not reach provider", w.calls)
	}
}

func TestHandlerFailedRequestKeepsLastCity(t *testing.T) {
	kazan := domain.Weather{City: "Казань", Country: "RU"}
	for _, failErr := range []error{domain.ErrCityNotFound, domain.ErrInvalidCity, domain.ErrProviderUnavailable} {
		t.Run(failErr.Error(), func(t *testing.T) {
			w := &fakeWeather{w: kazan}
			h, c := newClockedTestHandler(w)
			h.respond(t.Context(), 1, "", "Казань")

			w.err = fmt.Errorf("get current weather: %w", failErr)
			c.Advance(3 * time.Second)
			h.respond(t.Context(), 1, "/weather", "Нигде")

			w.err, w.gotCity = nil, ""
			c.Advance(3 * time.Second)
			if got := h.respond(t.Context(), 1, "/weather", ""); got != formatWeather(kazan) {
				t.Errorf("repeat reply = %q, want weather", got)
			}
			if w.gotCity != "Казань" {
				t.Errorf("weather got city %q, want %q", w.gotCity, "Казань")
			}
		})
	}
}

func TestHandlerFailedFirstRequestStoresNothing(t *testing.T) {
	w := &fakeWeather{err: domain.ErrCityNotFound}
	h, c := newClockedTestHandler(w)
	h.respond(t.Context(), 1, "", "Нигде")
	c.Advance(3 * time.Second)
	if got := h.respond(t.Context(), 1, "/weather", ""); got != msgEmptyCity {
		t.Errorf("reply = %q, want %q", got, msgEmptyCity)
	}
}

func TestHandlerLastCityPerChat(t *testing.T) {
	w := &fakeWeather{w: domain.Weather{City: "X"}}
	h, c := newClockedTestHandler(w)
	h.respond(t.Context(), 1, "", "Казань")
	h.respond(t.Context(), 2, "", "Пермь")
	c.Advance(3 * time.Second)
	for id, want := range map[int64]string{1: "Казань", 2: "Пермь"} {
		h.respond(t.Context(), id, "/weather", "")
		if w.gotCity != want {
			t.Errorf("chat %d: weather got city %q, want %q", id, w.gotCity, want)
		}
	}
}

func TestHandlerLongCityNotStored(t *testing.T) {
	w := &fakeWeather{w: domain.Weather{City: "X"}}
	h, c := newClockedTestHandler(w)
	h.respond(t.Context(), 1, "", strings.Repeat("а", maxStoredCityBytes))
	c.Advance(3 * time.Second)
	if got := h.respond(t.Context(), 1, "/weather", ""); got != msgEmptyCity {
		t.Errorf("reply = %q, want %q", got, msgEmptyCity)
	}
}
