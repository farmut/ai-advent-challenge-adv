// Package openweather — адаптер к OpenWeatherMap Current Weather API.
package openweather

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"weatherbot/internal/domain"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// Client запрашивает текущую погоду у OpenWeatherMap.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewClient создаёт клиент. httpClient обязан иметь Timeout.
func NewClient(baseURL, apiKey string, httpClient *http.Client) *Client {
	return &Client{baseURL: baseURL, apiKey: apiKey, httpClient: httpClient}
}

type currentResponse struct {
	Name string `json:"name"`
	Sys  struct {
		Country string `json:"country"`
	} `json:"sys"`
	Main struct {
		Temp      float64 `json:"temp"`
		FeelsLike float64 `json:"feels_like"`
		Humidity  int     `json:"humidity"`
	} `json:"main"`
	Weather []struct {
		Description string `json:"description"`
	} `json:"weather"`
	Wind struct {
		Speed float64 `json:"speed"`
		Deg   int     `json:"deg"`
	} `json:"wind"`
}

// Current возвращает текущую погоду в городе.
func (c *Client) Current(ctx context.Context, city string) (domain.Weather, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return domain.Weather{}, fmt.Errorf("parse base url: %w", err)
	}
	u = u.JoinPath("data", "2.5", "weather")
	q := url.Values{}
	q.Set("q", city)
	q.Set("units", "metric")
	q.Set("lang", "ru")
	q.Set("appid", c.apiKey)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return domain.Weather{}, fmt.Errorf("build request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return domain.Weather{}, fmt.Errorf("%w: %s", domain.ErrProviderUnavailable, c.redact(err.Error()))
	}
	defer resp.Body.Close()

	if err := statusError(resp.StatusCode); err != nil {
		return domain.Weather{}, err
	}
	var body currentResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&body); err != nil {
		return domain.Weather{}, fmt.Errorf("%w: decode response: %v", domain.ErrProviderUnavailable, err)
	}
	return toDomain(body), nil
}

func statusError(code int) error {
	switch {
	case code == http.StatusOK:
		return nil
	case code == http.StatusNotFound:
		return domain.ErrCityNotFound
	case code == http.StatusUnauthorized:
		return domain.ErrProviderAuth
	case code == http.StatusTooManyRequests:
		return domain.ErrRateLimited
	default:
		return fmt.Errorf("%w: status %d", domain.ErrProviderUnavailable, code)
	}
}

func toDomain(r currentResponse) domain.Weather {
	w := domain.Weather{
		City: r.Name, Country: r.Sys.Country,
		TempC: r.Main.Temp, FeelsLikeC: r.Main.FeelsLike, Humidity: r.Main.Humidity,
		WindSpeed: r.Wind.Speed, WindDeg: r.Wind.Deg,
	}
	if len(r.Weather) > 0 {
		w.Description = r.Weather[0].Description
	}
	return w
}

// redact убирает ключ (в сыром и URL-кодированном виде) из текста ошибки.
func (c *Client) redact(s string) string {
	if c.apiKey == "" {
		return s
	}
	s = strings.ReplaceAll(s, c.apiKey, "[REDACTED]")
	return strings.ReplaceAll(s, url.QueryEscape(c.apiKey), "[REDACTED]")
}