package bot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"weatherbot/internal/telegram"
	"weatherbot/internal/weather"
)

// --- фейки ---

type fakeWeather struct {
	result weather.Weather
	err    error

	mu    sync.Mutex
	calls []string
}

func (f *fakeWeather) Current(_ context.Context, city string) (weather.Weather, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, city)
	return f.result, f.err
}

type sentMessage struct {
	chatID int64
	text   string
}

type fakeSender struct {
	mu   sync.Mutex
	sent []sentMessage
}

func (f *fakeSender) SendMessage(_ context.Context, chatID int64, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentMessage{chatID, text})
	return nil
}

func (f *fakeSender) messages() []sentMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentMessage(nil), f.sent...)
}

// fakeUpdater по очереди отдаёт заранее заданные ответы, затем вызывает onDrained
// и блокируется до отмены контекста, как настоящий long polling.
type fakeUpdater struct {
	responses []updaterResponse
	onDrained func()

	mu      sync.Mutex
	offsets []int64
}

type updaterResponse struct {
	updates []telegram.Update
	err     error
}

func (f *fakeUpdater) GetUpdates(ctx context.Context, offset int64, _ int) ([]telegram.Update, error) {
	f.mu.Lock()
	f.offsets = append(f.offsets, offset)
	if len(f.responses) > 0 {
		r := f.responses[0]
		f.responses = f.responses[1:]
		f.mu.Unlock()
		return r.updates, r.err
	}
	f.mu.Unlock()

	if f.onDrained != nil {
		f.onDrained()
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestBot(u Updater, s Sender, w WeatherProvider) *Bot {
	b := New(u, s, w, Options{Logger: discardLogger()})
	b.minBackoff = time.Millisecond
	b.maxBackoff = time.Millisecond
	return b
}

var moscow = weather.Weather{
	City: "Moscow", Country: "RU", Description: "облачно",
	Temp: 5.34, FeelsLike: 2.1, Humidity: 80, Pressure: 1012, WindSpeed: 3.5,
}

// --- Reply ---

func TestReply(t *testing.T) {
	provider := &fakeWeather{result: moscow}
	b := newTestBot(nil, nil, provider)

	tests := []struct {
		name, input, want string
	}{
		{"start", "/start", helpText},
		{"help with bot name", "/help@WeatherBot", helpText},
		{"weather without city", "/weather", askCityText},
		{"unknown command", "/foo", unknownCmdText},
		{"plain city", "Москва", "Погода: Moscow, RU"},
		{"weather command", "/weather   Москва ", "Погода: Moscow, RU"},
		{"uppercase command", "/WEATHER Москва", "Погода: Moscow, RU"},
		{"too long city", strings.Repeat("я", maxCityLength+1), cityTooLongText},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := b.Reply(context.Background(), tt.input)
			if !strings.HasPrefix(got, tt.want) {
				t.Errorf("Reply(%q) = %q, want prefix %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestReply_PassesTrimmedCity(t *testing.T) {
	provider := &fakeWeather{result: moscow}
	b := newTestBot(nil, nil, provider)

	b.Reply(context.Background(), "/weather@Bot  Нижний Новгород  ")

	if len(provider.calls) != 1 || provider.calls[0] != "Нижний Новгород" {
		t.Errorf("calls = %q", provider.calls)
	}
}

func TestReply_Errors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"not found", weather.ErrCityNotFound, "Город «Атлантида» не найден"},
		{"rate limited", weather.ErrRateLimited, rateLimitedText},
		{"unauthorized", weather.ErrUnauthorized, failureText},
		{"other", errors.New("boom"), failureText},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newTestBot(nil, nil, &fakeWeather{err: tt.err})
			got := b.Reply(context.Background(), "Атлантида")
			if !strings.HasPrefix(got, tt.want) {
				t.Errorf("got %q, want prefix %q", got, tt.want)
			}
		})
	}
}

// --- formatWeather ---

func TestFormatWeather(t *testing.T) {
	got := formatWeather(moscow, "metric")
	want := "Погода: Moscow, RU\n" +
		"Облачно\n" +
		"🌡 Температура: 5.3°C (ощущается как 2.1°C)\n" +
		"💧 Влажность: 80%\n" +
		"💨 Ветер: 3.5 м/с\n" +
		"🧭 Давление: 1012 гПа"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatWeather_Imperial(t *testing.T) {
	got := formatWeather(weather.Weather{City: "NYC"}, "imperial")
	if !strings.Contains(got, "°F") || !strings.Contains(got, "миль/ч") || strings.Contains(got, ", ") {
		t.Errorf("unexpected output: %s", got)
	}
}

// --- Run ---

func runBot(t *testing.T, b *Bot, u *fakeUpdater) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	u.onDrained = cancel

	done := make(chan error, 1)
	go func() { done <- b.Run(ctx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("Run did not stop")
	}
}

func TestRun_HandlesUpdatesAndAdvancesOffset(t *testing.T) {
	updater := &fakeUpdater{responses: []updaterResponse{
		{updates: []telegram.Update{
			{UpdateID: 10, Message: &telegram.Message{Chat: telegram.Chat{ID: 1}, Text: "Москва"}},
			{UpdateID: 11}, // не сообщение — пропускается
			{UpdateID: 12, Message: &telegram.Message{Chat: telegram.Chat{ID: 2}, Text: "/help"}},
		}},
	}}
	sender := &fakeSender{}
	b := newTestBot(updater, sender, &fakeWeather{result: moscow})

	runBot(t, b, updater)

	if got := updater.offsets; len(got) != 2 || got[0] != 0 || got[1] != 13 {
		t.Errorf("offsets = %v, want [0 13]", got)
	}
	sent := sender.messages()
	if len(sent) != 2 {
		t.Fatalf("sent %d messages, want 2: %+v", len(sent), sent)
	}
	byChat := map[int64]string{}
	for _, m := range sent {
		byChat[m.chatID] = m.text
	}
	if !strings.HasPrefix(byChat[1], "Погода: Moscow") || byChat[2] != helpText {
		t.Errorf("unexpected replies: %+v", byChat)
	}
}

func TestRun_RetriesAfterError(t *testing.T) {
	updater := &fakeUpdater{responses: []updaterResponse{
		{err: errors.New("network down")},
		{updates: []telegram.Update{
			{UpdateID: 5, Message: &telegram.Message{Chat: telegram.Chat{ID: 1}, Text: "/start"}},
		}},
	}}
	sender := &fakeSender{}
	b := newTestBot(updater, sender, &fakeWeather{})

	runBot(t, b, updater)

	if got := updater.offsets; len(got) != 3 || got[0] != 0 || got[1] != 0 || got[2] != 6 {
		t.Errorf("offsets = %v, want [0 0 6]", got)
	}
	if len(sender.messages()) != 1 {
		t.Errorf("want 1 message after retry, got %+v", sender.messages())
	}
}

func TestNew_Defaults(t *testing.T) {
	b := New(nil, nil, nil, Options{})
	if b.units != "metric" || b.requestTimeout != 10*time.Second || b.pollTimeoutSec != 30 || cap(b.sem) != 16 || b.log == nil {
		t.Errorf("unexpected defaults: %+v", b)
	}
}
