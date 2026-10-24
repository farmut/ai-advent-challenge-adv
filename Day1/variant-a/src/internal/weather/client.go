// Package weather — клиент OpenWeatherMap Current Weather API.
package weather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"weatherbot/internal/httpx"
)

// DefaultBaseURL — адрес OpenWeatherMap API 2.5.
const DefaultBaseURL = "https://api.openweathermap.org/data/2.5"

// Ограничение на размер ответа, чтобы не читать в память бесконечный поток.
const maxResponseBytes = 1 << 20

var (
	// ErrCityNotFound — город не найден.
	ErrCityNotFound = errors.New("city not found")
	// ErrUnauthorized — неверный или неактивный API-ключ.
	ErrUnauthorized = errors.New("invalid OpenWeatherMap API key")
	// ErrRateLimited — превышен лимит запросов.
	ErrRateLimited = errors.New("OpenWeatherMap rate limit exceeded")
)

// Weather — текущая погода в городе.
type Weather struct {
	City        string
	Country     string
	Description string
	Temp        float64
	FeelsLike   float64
	Humidity    int
	Pressure    int
	WindSpeed   float64
}

// Client обращается к OpenWeatherMap.
type Client struct {
	baseURL    string
	apiKey     string
	units      string
	lang       string
	httpClient *http.Client
}

// NewClient создаёт клиента. Пустой baseURL означает DefaultBaseURL.
func NewClient(httpClient *http.Client, baseURL, apiKey, units, lang string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		units:      units,
		lang:       lang,
		httpClient: httpClient,
	}
}

// apiResponse описывает нужные поля ответа /weather.
type apiResponse struct {
	Name    string `json:"name"`
	Message string `json:"message"`
	Sys     struct {
		Country string `json:"country"`
	} `json:"sys"`
	Weather []struct {
		Description string `json:"description"`
	} `json:"weather"`
	Main struct {
		Temp      float64 `json:"temp"`
		FeelsLike float64 `json:"feels_like"`
		Humidity  int     `json:"humidity"`
		Pressure  int     `json:"pressure"`
	} `json:"main"`
	Wind struct {
		Speed float64 `json:"speed"`
	} `json:"wind"`
}

// Current возвращает текущую погоду в городе.
func (c *Client) Current(ctx context.Context, city string) (Weather, error) {
	city = strings.TrimSpace(city)
	if city == "" {
		return Weather{}, ErrCityNotFound
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.requestURL(city), nil)
	if err != nil {
		return Weather{}, fmt.Errorf("build weather request: %w", httpx.RedactURLError(err))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Weather{}, fmt.Errorf("request weather: %w", httpx.RedactURLError(err))
	}
	defer resp.Body.Close()

	var body apiResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&body); err != nil {
		return Weather{}, fmt.Errorf("decode weather response (status %d): %w", resp.StatusCode, err)
	}

	if err := statusError(resp.StatusCode, body.Message); err != nil {
		return Weather{}, err
	}
	return body.toWeather(), nil
}

func (c *Client) requestURL(city string) string {
	q := url.Values{}
	q.Set("q", city)
	q.Set("appid", c.apiKey)
	q.Set("units", c.units)
	q.Set("lang", c.lang)
	return c.baseURL + "/weather?" + q.Encode()
}

func statusError(status int, message string) error {
	switch {
	case status == http.StatusOK:
		return nil
	case status == http.StatusNotFound:
		return ErrCityNotFound
	case status == http.StatusUnauthorized:
		return ErrUnauthorized
	case status == http.StatusTooManyRequests:
		return ErrRateLimited
	default:
		return fmt.Errorf("unexpected OpenWeatherMap status %d: %s", status, message)
	}
}

func (r apiResponse) toWeather() Weather {
	w := Weather{
		City:      r.Name,
		Country:   r.Sys.Country,
		Temp:      r.Main.Temp,
		FeelsLike: r.Main.FeelsLike,
		Humidity:  r.Main.Humidity,
		Pressure:  r.Main.Pressure,
		WindSpeed: r.Wind.Speed,
	}
	if len(r.Weather) > 0 {
		w.Description = r.Weather[0].Description
	}
	return w
}
