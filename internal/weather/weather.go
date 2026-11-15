package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Data struct {
	Location    string    `json:"location"`
	Temperature float64   `json:"temperature"`
	Apparent    float64   `json:"apparent"`
	Humidity    int       `json:"humidity"`
	WindSpeed   float64   `json:"windSpeed"`
	WeatherCode int       `json:"weatherCode"`
	IsDay       bool      `json:"isDay"`
	High        float64   `json:"high"`
	Low         float64   `json:"low"`
	Units       string    `json:"units"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Client struct {
	http  *http.Client
	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	data    Data
	expires time.Time
}

func New() *Client {
	return &Client{
		http:  &http.Client{Timeout: 10 * time.Second},
		cache: map[string]cacheEntry{},
	}
}

func (c *Client) Get(ctx context.Context, latitude, longitude float64, units, location string) (Data, error) {
	if units == "" {
		units = "metric"
	}
	key := fmt.Sprintf("%.4f|%.4f|%s|%s", latitude, longitude, units, strings.TrimSpace(location))
	c.mu.Lock()
	if entry, ok := c.cache[key]; ok && time.Now().Before(entry.expires) {
		c.mu.Unlock()
		return entry.data, nil
	}
	c.mu.Unlock()

	query := url.Values{}
	query.Set("latitude", strconv.FormatFloat(latitude, 'f', 4, 64))
	query.Set("longitude", strconv.FormatFloat(longitude, 'f', 4, 64))
	query.Set("current", "temperature_2m,relative_humidity_2m,apparent_temperature,is_day,precipitation,weather_code,wind_speed_10m")
	query.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min")
	query.Set("timezone", "auto")
	query.Set("forecast_days", "1")
	if units == "imperial" {
		query.Set("temperature_unit", "fahrenheit")
		query.Set("wind_speed_unit", "mph")
		query.Set("precipitation_unit", "inch")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.open-meteo.com/v1/forecast?"+query.Encode(), nil)
	if err != nil {
		return Data{}, err
	}
	req.Header.Set("User-Agent", "Blackflame/0.1")
	resp, err := c.http.Do(req)
	if err != nil {
		return Data{}, fmt.Errorf("weather request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Data{}, fmt.Errorf("weather service returned %d", resp.StatusCode)
	}
	var payload struct {
		Current struct {
			Temperature2m       float64 `json:"temperature_2m"`
			RelativeHumidity2m  int     `json:"relative_humidity_2m"`
			ApparentTemperature float64 `json:"apparent_temperature"`
			IsDay               int     `json:"is_day"`
			WeatherCode         int     `json:"weather_code"`
			WindSpeed10m        float64 `json:"wind_speed_10m"`
		} `json:"current"`
		Daily struct {
			WeatherCode      []int     `json:"weather_code"`
			Temperature2mMax []float64 `json:"temperature_2m_max"`
			Temperature2mMin []float64 `json:"temperature_2m_min"`
		} `json:"daily"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Data{}, fmt.Errorf("decode weather: %w", err)
	}
	data := Data{
		Location:    strings.TrimSpace(location),
		Temperature: payload.Current.Temperature2m,
		Apparent:    payload.Current.ApparentTemperature,
		Humidity:    payload.Current.RelativeHumidity2m,
		WindSpeed:   payload.Current.WindSpeed10m,
		WeatherCode: payload.Current.WeatherCode,
		IsDay:       payload.Current.IsDay == 1,
		Units:       units,
		UpdatedAt:   time.Now().UTC(),
	}
	if len(payload.Daily.Temperature2mMax) > 0 {
		data.High = payload.Daily.Temperature2mMax[0]
	}
	if len(payload.Daily.Temperature2mMin) > 0 {
		data.Low = payload.Daily.Temperature2mMin[0]
	}

	c.mu.Lock()
	c.cache[key] = cacheEntry{data: data, expires: time.Now().Add(10 * time.Minute)}
	c.mu.Unlock()
	return data, nil
}
