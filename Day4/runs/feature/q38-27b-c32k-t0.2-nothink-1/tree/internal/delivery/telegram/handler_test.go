package telegram

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"weatherbot/internal/domain"
)

type fakeWeather struct {
	weather domain.Weather
	err     error
	calls   int
}

func (f *fakeWeather) Current(_ context.Context, _ string) (domain.Weather, error) {
	f.calls++
	return f.weather, f.err
}

type fakeSender struct {
	sent   []string
	err    error
	chatID int64
}

func (f *fakeSender) Send(_ context.Context, chatID int64, text string) error {
	f.chatID = chatID
	f.sent = append(f.sent, text)
	return f.err
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestHandleCommands(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "start", text: "/start", want: msgStart},
		{name: "help", text: "/help", want: msgHelp},
		{name: "unknown command", text: "/foo bar", want: msgHelp},
		{name: "weather without city", text: "/weather", want: msgEmptyCity},
		{name: "weather with spaces only", text: "/weather   ", want: msgEmptyCity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sender := &fakeSender{}
			h := NewHandler(&fakeWeather{}, sender, testLogger())
			h.Handle(context.Background(), 1, tt.text)
			if len(sender.sent) != 1 || sender.sent[0] != tt.want {
				t.Errorf("sent = %v, want [%q]", sender.sent, tt.want)
			}
		})
	}
}

func TestHandleWeatherSuccess(t *testing.T) {
	w := domain.Weather{City: "Москва", Country: "RU", TempC: 12.4, FeelsLikeC: 9.6,
		Description: "переменная облачность", Humidity: 71, WindSpeed: 4.2, WindDeg: 315}
	sender := &fakeSender{}
	h := NewHandler(&fakeWeather{weather: w}, sender, testLogger())
	h.Handle(context.Background(), 1, "/weather Казань")
	want := "🌤 Москва, RU\n🌡 +12 °C, ощущается как +10 °C\n☁️ Переменная облачность\n💧 Влажность 71 %\n💨 Ветер 4 м/с, СЗ"
	if len(sender.sent) != 1 || sender.sent[0] != want {
		t.Errorf("sent = %v, want [%q]", sender.sent, want)
	}
}

func TestHandlePlainTextIsCity(t *testing.T) {
	w := domain.Weather{City: "Казань", Country: "RU"}
	fw := &fakeWeather{weather: w}
	sender := &fakeSender{}
	h := NewHandler(fw, sender, testLogger())
	h.Handle(context.Background(), 1, "Казань")
	if fw.calls != 1 {
		t.Fatalf("weather calls = %d, want 1", fw.calls)
	}
	if len(sender.sent) != 1 || !containsRune(sender.sent[0], 'К') {
		t.Errorf("sent = %v", sender.sent)
	}
}

func TestHandleBotsCommandNormalization(t *testing.T) {
	w := domain.Weather{City: "Казань", Country: "RU"}
	fw := &fakeWeather{weather: w}
	sender := &fakeSender{}
	h := NewHandler(fw, sender, testLogger())
	h.Handle(context.Background(), 1, "/weather@MyBot Казань")
	if fw.calls != 1 {
		t.Fatalf("weather calls = %d, want 1", fw.calls)
	}
}

func TestHandleCityNotFound(t *testing.T) {
	fw := &fakeWeather{err: domain.ErrCityNotFound}
	sender := &fakeSender{}
	h := NewHandler(fw, sender, testLogger())
	h.Handle(context.Background(), 1, "/weather Нигдеград")
	if len(sender.sent) != 1 || sender.sent[0] != msgCityNotFound("Нигдеград") {
		t.Errorf("sent = %v", sender.sent)
	}
}

func TestHandleInvalidCity(t *testing.T) {
	fw := &fakeWeather{err: domain.ErrInvalidCity}
	sender := &fakeSender{}
	h := NewHandler(fw, sender, testLogger())
	h.Handle(context.Background(), 1, "/weather city 123")
	if len(sender.sent) != 1 || sender.sent[0] != msgInvalidCity {
		t.Errorf("sent = %v", sender.sent)
	}
}

func TestHandleProviderErrors(t *testing.T) {
	for name, err := range map[string]error{
		"unavailable": domain.ErrProviderUnavailable,
		"auth":        domain.ErrProviderAuth,
		"rate":        domain.ErrRateLimited,
	} {
		t.Run(name, func(t *testing.T) {
			sender := &fakeSender{}
			h := NewHandler(&fakeWeather{err: err}, sender, testLogger())
			h.Handle(context.Background(), 1, "/weather Казань")
			if len(sender.sent) != 1 || sender.sent[0] != msgUnavailable {
				t.Errorf("sent = %v, want [%q]", sender.sent, msgUnavailable)
			}
		})
	}
}

func TestHandleRateLimitPerChat(t *testing.T) {
	sender := &fakeSender{}
	h := NewHandler(&fakeWeather{}, sender, testLogger())
	h.Handle(context.Background(), 1, "/start")
	h.Handle(context.Background(), 1, "/start")
	if len(sender.sent) != 2 || sender.sent[1] != msgTooFrequent {
		t.Errorf("sent = %v", sender.sent)
	}
	// Другой чат не ограничен.
	h.Handle(context.Background(), 2, "/start")
	if len(sender.sent) != 3 || sender.sent[2] != msgStart {
		t.Errorf("sent = %v", sender.sent)
	}
}

func TestHandleRateLimitExpires(t *testing.T) {
	sender := &fakeSender{}
	h := NewHandler(&fakeWeather{}, sender, testLogger())
	h.Handle(context.Background(), 1, "/start")
	time.Sleep(perChatInterval + 10*time.Millisecond)
	h.Handle(context.Background(), 1, "/start")
	if len(sender.sent) != 2 || sender.sent[1] != msgStart {
		t.Errorf("sent = %v", sender.sent)
	}
}

func TestHandlePanicRecovered(t *testing.T) {
	sender := &fakeSender{}
	h := NewHandler(&panickingWeather{}, sender, testLogger())
	h.Handle(context.Background(), 1, "/weather Казань") // не должно паниковать
	if len(sender.sent) != 1 {
		t.Fatalf("sent = %v, want one reply", sender.sent)
	}
}

type panickingWeather struct{}

func (p *panickingWeather) Current(_ context.Context, _ string) (domain.Weather, error) {
	panic("boom")
}

func TestHandleSendErrorDoesNotPanic(t *testing.T) {
	sender := &fakeSender{err: errors.New("send failed")}
	h := NewHandler(&fakeWeather{}, sender, testLogger())
	h.Handle(context.Background(), 1, "/start")
	if len(sender.sent) != 1 {
		t.Errorf("sent = %v", sender.sent)
	}
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}
