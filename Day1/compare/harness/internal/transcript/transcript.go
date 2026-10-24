// Package transcript описывает формат transcripts/{a,b}.json, общий для
// harness (пишет) и render (читает).
package transcript

import "time"

// Run — результат прогона одного варианта бота.
type Run struct {
	Variant    string         `json:"variant"`
	StartedAt  time.Time      `json:"started_at"`
	DurationMs int64          `json:"duration_ms"`
	Source     string         `json:"source"`
	Build      Build          `json:"build"`
	Mode       string         `json:"mode"`      // env | docker-https-proxy-mitm | untestable
	ModeNote   string         `json:"mode_note"` // человекочитаемое описание способа подмены
	EnvNames   EnvNames       `json:"env_names"`
	Attempts   []Attempt      `json:"attempts"`
	Scenarios  []Scenario     `json:"scenarios"`
	Leaks      Leaks          `json:"leaks"`
	Shutdown   *Shutdown      `json:"shutdown,omitempty"`           // основной прогон (сценарии 1–14)
	Shutdown2  *Shutdown      `json:"shutdown_wrong_key,omitempty"` // прогон с неверным ключом (сценарий 15)
	TGRequests map[string]int `json:"telegram_requests"`            // метод Bot API -> число вызовов
	Notes      []string       `json:"notes,omitempty"`
}

// Build — результат go build.
type Build struct {
	OK     bool   `json:"ok"`
	Output string `json:"output,omitempty"`
	Binary string `json:"binary,omitempty"`
}

// EnvNames — имена переменных окружения, выставленные боту.
type EnvNames struct {
	Found       []string `json:"found_in_source"` // строковые литералы *_URL и т.п. из исходников
	TelegramURL []string `json:"telegram_base_url_vars"`
	OWMURL      []string `json:"owm_base_url_vars"`
}

// Attempt — попытка направить бота на фейковые серверы.
type Attempt struct {
	Name      string `json:"name"`
	OK        bool   `json:"ok"`
	Detail    string `json:"detail"`
	ElapsedMs int64  `json:"elapsed_ms"`
}

// Scenario — один сценарий переписки.
type Scenario struct {
	ID           int        `json:"id"`
	Title        string     `json:"title"`
	ChatID       int64      `json:"chat_id"`
	Inputs       []string   `json:"inputs"`
	Replies      []Reply    `json:"replies"`
	OWMQueries   []OWMQuery `json:"owm_queries"`
	FirstReplyMs *int64     `json:"first_reply_ms"` // nil — ответа не было
	WaitedMs     int64      `json:"waited_ms"`
	Flags        Flags      `json:"flags"`
	Note         string     `json:"note,omitempty"`
}

// Reply — вызов sendMessage ботом.
type Reply struct {
	ChatID    int64  `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
	AtMs      int64  `json:"at_ms"` // от момента выдачи апдейта
}

// OWMQuery — запрос, пришедший в фейковый OpenWeatherMap.
type OWMQuery struct {
	Path        string   `json:"path"`
	RawQuery    string   `json:"raw_query"` // appid заменён на <KEY>/<WRONG_KEY>
	Q           []string `json:"q"`
	AppIDCount  int      `json:"appid_count"`
	AppIDValid  bool     `json:"appid_valid"`
	Status      int      `json:"status"`
	BytesSent   int64    `json:"bytes_sent"`
	DurationMs  int64    `json:"duration_ms"`
	ClientAbort bool     `json:"client_aborted"`
}

// Flags — отметки для рендера.
type Flags struct {
	Replied     bool    `json:"replied"`
	LeakInReply bool    `json:"leak_in_reply"`
	Injection   *string `json:"injection,omitempty"` // safe-escaped | safe-rejected | VULNERABLE
	Expect      *bool   `json:"expect,omitempty"`    // соответствует ли ожидаемому поведению
	ExpectNote  string  `json:"expect_note,omitempty"`
}

// Leaks — число вхождений секретов.
type Leaks struct {
	TokenInLog     int `json:"token_in_log"`
	KeyInLog       int `json:"key_in_log"`
	TokenInReplies int `json:"token_in_replies"`
	KeyInReplies   int `json:"key_in_replies"`
}

// Shutdown — реакция на SIGTERM.
type Shutdown struct {
	Signal    string `json:"signal"`
	Graceful  bool   `json:"graceful"` // завершился сам за 5 с
	ElapsedMs int64  `json:"elapsed_ms"`
	ExitCode  int    `json:"exit_code"`
	Killed    bool   `json:"killed"`
}
