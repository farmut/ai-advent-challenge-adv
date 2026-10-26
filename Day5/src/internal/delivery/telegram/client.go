// Package telegram — delivery-слой: long polling Telegram Bot API и обработка команд.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	maxBodyBytes    = 1 << 20 // 1 MiB
	longPollTimeout = 30      // секунд; http.Client.Timeout должен быть больше
)

// ErrUnauthorized — Telegram отверг токен бота; ретраи бессмысленны.
var ErrUnauthorized = errors.New("telegram bot token rejected")

// Update — входящее обновление Bot API (нужны только сообщения).
type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message"`
}

// Message — входящее сообщение.
type Message struct {
	Chat Chat   `json:"chat"`
	Text string `json:"text"`
}

// Chat — чат, из которого пришло сообщение.
type Chat struct {
	ID int64 `json:"id"`
}

// User — аккаунт Telegram; для бота нужен только username.
type User struct {
	Username string `json:"username"`
}

// retryAfterError — Telegram попросил подождать (HTTP 429).
type retryAfterError struct {
	after time.Duration
}

func (e *retryAfterError) Error() string {
	return "telegram rate limited, retry after " + e.after.String()
}

// Client — минимальный клиент Telegram Bot API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

type sendMessageRequest struct {
	ChatID int64  `json:"chat_id"`
	Text   string `json:"text"`
}

// NewClient создаёт клиент. Timeout у httpClient должен превышать long-poll timeout (30 с).
func NewClient(baseURL, token string, httpClient *http.Client) *Client {
	return &Client{baseURL: baseURL, token: token, httpClient: httpClient}
}

// GetUpdates забирает новые сообщения через long polling.
func (c *Client) GetUpdates(ctx context.Context, offset int64) ([]Update, error) {
	var updates []Update
	if err := c.call(ctx, http.MethodGet, "getUpdates", updatesQuery(offset, longPollTimeout), nil, &updates); err != nil {
		return nil, fmt.Errorf("get updates: %w", err)
	}
	return updates, nil
}

// Ack подтверждает апдейты с update_id < offset, не дожидаясь новых (timeout=0).
func (c *Client) Ack(ctx context.Context, offset int64) error {
	q := updatesQuery(offset, 0)
	q.Set("limit", "1") // ответ не нужен, хватит минимального
	if err := c.call(ctx, http.MethodGet, "getUpdates", q, nil, nil); err != nil {
		return fmt.Errorf("ack updates: %w", err)
	}
	return nil
}

// Me возвращает аккаунт бота, которому принадлежит токен (getMe).
func (c *Client) Me(ctx context.Context) (User, error) {
	var u User
	if err := c.call(ctx, http.MethodGet, "getMe", nil, nil, &u); err != nil {
		return User{}, fmt.Errorf("get me: %w", err)
	}
	return u, nil
}

// Send отправляет plain-text сообщение (без parse_mode). Ошибка уже очищена от токена.
func (c *Client) Send(ctx context.Context, chatID int64, text string) error {
	body, err := json.Marshal(sendMessageRequest{ChatID: chatID, Text: text})
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}
	if err := c.call(ctx, http.MethodPost, "sendMessage", nil, body, nil); err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	return nil
}

func (c *Client) call(ctx context.Context, method, apiMethod string, q url.Values, body []byte, result any) error {
	req, err := c.newRequest(ctx, method, apiMethod, q, body)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %s", c.redact(err.Error())) // *url.Error содержит токен в пути
	}
	defer resp.Body.Close()
	r, decErr := decodeAPIResponse(resp.Body) // 429 и прочие ошибки разбираются best-effort
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return &retryAfterError{after: time.Duration(r.Parameters.RetryAfter) * time.Second}
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%w: status %d", ErrUnauthorized, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("status %d: %s", resp.StatusCode, c.redact(r.Description))
	case decErr != nil:
		return fmt.Errorf("decode response: %w", decErr)
	case !r.OK:
		return fmt.Errorf("api error: %s", c.redact(r.Description))
	}
	if result != nil {
		if err := json.Unmarshal(r.Result, result); err != nil {
			return fmt.Errorf("decode result: %w", err)
		}
	}
	return nil
}

// newRequest собирает запрос к методу Bot API; JSON-тело — только при body != nil.
func (c *Client) newRequest(ctx context.Context, method, apiMethod string, q url.Values, body []byte) (*http.Request, error) {
	u, err := c.methodURL(apiMethod, q)
	if err != nil {
		return nil, err
	}
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %s", c.redact(err.Error()))
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// methodURL возвращает URL метода Bot API. Содержит токен — не логировать.
func (c *Client) methodURL(apiMethod string, q url.Values) (string, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("parse base url: %w", err)
	}
	u = u.JoinPath("bot"+c.token, apiMethod) // формат токена проверен в config
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// decodeAPIResponse читает конверт ответа Bot API, не более maxBodyBytes.
func decodeAPIResponse(r io.Reader) (apiResponse, error) {
	var resp apiResponse
	err := json.NewDecoder(io.LimitReader(r, maxBodyBytes)).Decode(&resp)
	return resp, err
}

func updatesQuery(offset int64, timeout int) url.Values {
	q := url.Values{}
	q.Set("offset", strconv.FormatInt(offset, 10))
	q.Set("timeout", strconv.Itoa(timeout))
	q.Set("allowed_updates", `["message"]`)
	return q
}

// redact убирает токен (в сыром и экранированном виде) из текста ошибки.
func (c *Client) redact(s string) string {
	if c.token == "" {
		return s
	}
	for _, v := range []string{c.token, url.PathEscape(c.token), url.QueryEscape(c.token)} {
		s = strings.ReplaceAll(s, v, "[REDACTED]")
	}
	return s
}
