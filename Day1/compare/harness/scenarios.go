package main

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"harness/internal/transcript"
)

const (
	chatMain  int64 = 1001
	chatFlood int64 = 2002

	// Пауза между сообщениями одного чата: вариант B режет чаще 1 сообщения в 2 с
	// («Не так быстро»). Сценарии 1–13 проверяют функциональность, поэтому шлём
	// их с человеческим темпом; лимит проверяется отдельно сценарием 14.
	perChatGap = 2200 * time.Millisecond
)

const injectionText = "a&appid=x#"

// scenarioDef — описание сценария до прогона.
type scenarioDef struct {
	id      int
	title   string
	chat    int64
	texts   []string
	replies int           // сколько ответов ждём
	wait    time.Duration // 0 — значение по умолчанию (-wait)
	check   func(sc *transcript.Scenario)
}

func scenarios() []scenarioDef {
	long := strings.Repeat("а", 500)
	flood := make([]string, 20)
	for i := range flood {
		flood[i] = "Москва"
	}
	return []scenarioDef{
		{id: 1, title: "/start", chat: chatMain, texts: []string{"/start"}, replies: 1,
			check: expect(func(sc *transcript.Scenario) (bool, string) { return replied(sc), "есть приветствие" })},
		{id: 2, title: "/help", chat: chatMain, texts: []string{"/help"}, replies: 1,
			check: expect(func(sc *transcript.Scenario) (bool, string) {
				return anyReply(sc, "/weather"), "справка упоминает /weather"
			})},
		{id: 3, title: "Город текстом: Москва", chat: chatMain, texts: []string{"Москва"}, replies: 1,
			check: expect(moscowOK)},
		{id: 4, title: "/weather London", chat: chatMain, texts: []string{"/weather London"}, replies: 1,
			check: expect(func(sc *transcript.Scenario) (bool, string) {
				return anyReply(sc, "Лондон") && len(sc.OWMQueries) == 1 && slices.Equal(sc.OWMQueries[0].Q, []string{"London"}), "погода Лондона"
			})},
		{id: 5, title: "/weather без города", chat: chatMain, texts: []string{"/weather"}, replies: 1,
			check: expect(func(sc *transcript.Scenario) (bool, string) {
				return replied(sc) && len(sc.OWMQueries) == 0, "подсказка, без запроса в OWM"
			})},
		{id: 6, title: "Несуществующий город (OWM 404)", chat: chatMain, texts: []string{"Nowhere"}, replies: 1,
			check: expect(func(sc *transcript.Scenario) (bool, string) {
				return anyReply(sc, "Nowhere") && !looksLikeWeather(sc), "«город Nowhere не найден»"
			})},
		{id: 7, title: "OWM 500", chat: chatMain, texts: []string{"Error500"}, replies: 1,
			check: expect(func(sc *transcript.Scenario) (bool, string) {
				return replied(sc) && !looksLikeWeather(sc), "понятная ошибка, без погоды"
			})},
		{id: 8, title: "OWM 429", chat: chatMain, texts: []string{"RateLimit"}, replies: 1,
			check: expect(func(sc *transcript.Scenario) (bool, string) {
				return replied(sc) && !looksLikeWeather(sc), "понятная ошибка, без погоды"
			})},
		{id: 9, title: "Инъекция в query", chat: chatMain, texts: []string{injectionText}, replies: 1, check: checkInjection},
		{id: 10, title: "500 символов", chat: chatMain, texts: []string{long}, replies: 1,
			check: expect(func(sc *transcript.Scenario) (bool, string) {
				return replied(sc) && len(sc.OWMQueries) == 0, "отказ без запроса в OWM"
			})},
		{id: 11, title: "Медленный OWM (25 с)", chat: chatMain, texts: []string{"Slow"}, replies: 1, wait: 40 * time.Second,
			check: expect(func(sc *transcript.Scenario) (bool, string) {
				ok := sc.FirstReplyMs != nil && *sc.FirstReplyMs <= 15000 && !looksLikeWeather(sc)
				return ok, "таймаут ≤15 с и сообщение об ошибке"
			})},
		{id: 12, title: "Огромный ответ OWM (50 MiB)", chat: chatMain, texts: []string{"Huge"}, replies: 1, wait: 20 * time.Second,
			check: expect(func(sc *transcript.Scenario) (bool, string) {
				bounded := len(sc.OWMQueries) > 0
				for _, q := range sc.OWMQueries {
					if q.BytesSent >= 40<<20 {
						bounded = false
					}
				}
				return replied(sc) && bounded, "тело ограничено, ответ об ошибке"
			})},
		{id: 13, title: "/weather@WeatherBot Москва", chat: chatMain, texts: []string{"/weather@WeatherBot Москва"}, replies: 1,
			check: expect(moscowOK)},
		{id: 14, title: "Flood: 20×«Москва» из чата 2002", chat: chatFlood, texts: flood, replies: 20, wait: 15 * time.Second,
			check: func(sc *transcript.Scenario) {
				sc.Note = "запросов в OWM: " + strconv.Itoa(len(sc.OWMQueries)) + ", ответов: " + strconv.Itoa(len(sc.Replies))
			}},
	}
}

// wrongKeyScenario — сценарий 15, отдельный прогон с неверным ключом OWM.
func wrongKeyScenario() scenarioDef {
	return scenarioDef{id: 15, title: "Неверный ключ OWM (401)", chat: chatMain, texts: []string{"Москва"}, replies: 1,
		check: expect(func(sc *transcript.Scenario) (bool, string) {
			return replied(sc) && !looksLikeWeather(sc) && !sc.Flags.LeakInReply, "ошибка без погоды и без ключа"
		})}
}

func expect(f func(sc *transcript.Scenario) (bool, string)) func(sc *transcript.Scenario) {
	return func(sc *transcript.Scenario) {
		ok, note := f(sc)
		sc.Flags.Expect = &ok
		sc.Flags.ExpectNote = note
	}
}

func moscowOK(sc *transcript.Scenario) (bool, string) {
	return anyReply(sc, "Москва") && anyReply(sc, "12") && len(sc.OWMQueries) == 1, "погода Москвы (12.3 °C), 1 запрос"
}

func checkInjection(sc *transcript.Scenario) {
	verdict := "safe-escaped"
	switch {
	case len(sc.OWMQueries) == 0:
		verdict = "safe-rejected"
	default:
		for _, q := range sc.OWMQueries {
			if q.AppIDCount != 1 || !q.AppIDValid || len(q.Q) != 1 || q.Q[0] != injectionText {
				verdict = "VULNERABLE"
			}
		}
	}
	sc.Flags.Injection = &verdict
	ok := verdict != "VULNERABLE" && replied(sc)
	sc.Flags.Expect = &ok
	sc.Flags.ExpectNote = "q не ломает query, appid один"
}

func replied(sc *transcript.Scenario) bool { return len(sc.Replies) > 0 }

func anyReply(sc *transcript.Scenario, sub string) bool {
	for _, r := range sc.Replies {
		if strings.Contains(r.Text, sub) {
			return true
		}
	}
	return false
}

// looksLikeWeather — ответ похож на сводку погоды (градусы).
func looksLikeWeather(sc *transcript.Scenario) bool {
	return anyReply(sc, "°")
}
