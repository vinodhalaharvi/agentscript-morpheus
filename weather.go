package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// WeatherData holds current weather + forecast
type WeatherData struct {
	Location   string
	Latitude   float64
	Longitude  float64
	Timezone   string
	Current    CurrentWeather
	HourlyNext []HourlyWeather
	DailyNext  []DailyWeather
}

type CurrentWeather struct {
	Temperature   float64
	FeelsLike     float64
	Humidity      int
	WindSpeed     float64
	WindDirection int
	Condition     string
	ConditionCode int
	IsDay         bool
}

type HourlyWeather struct {
	Time          string
	Temperature   float64
	Humidity      int
	RainChance    int
	WindSpeed     float64
	Condition     string
	ConditionCode int
}

type DailyWeather struct {
	Date         string
	TempMax      float64
	TempMin      float64
	RainChance   int
	RainSum      float64
	WindSpeedMax float64
	Condition    string
	Sunrise      string
	Sunset       string
}

// WeatherClient handles weather API calls
type WeatherClient struct {
	client  *http.Client
	verbose bool
}

// NewWeatherClient creates a new weather client
func NewWeatherClient(verbose bool) *WeatherClient {
	return &WeatherClient{
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		verbose: verbose,
	}
}

func (wc *WeatherClient) log(format string, args ...any) {
	if wc.verbose {
		fmt.Printf("[WEATHER] "+format+"\n", args...)
	}
}

// GetWeather fetches weather for a location name
func (wc *WeatherClient) GetWeather(ctx context.Context, location string) (*WeatherData, error) {
	// Step 1: Geocode the location name to lat/lon
	lat, lon, resolvedName, err := wc.geocode(ctx, location)
	if err != nil {
		return nil, fmt.Errorf("could not find location %q: %w", location, err)
	}

	wc.log("Resolved %q -> %s (%.4f, %.4f)", location, resolvedName, lat, lon)

	// Step 2: Fetch weather data from Open-Meteo
	weather, err := wc.fetchWeather(ctx, lat, lon)
	if err != nil {
		return nil, err
	}
	weather.Location = resolvedName

	return weather, nil
}

// geocode converts a location name to lat/lon using Open-Meteo geocoding API
func (wc *WeatherClient) geocode(ctx context.Context, location string) (lat, lon float64, name string, err error) {
	geocodeURL := fmt.Sprintf(
		"https://geocoding-api.open-meteo.com/v1/search?name=%s&count=1&language=en&format=json",
		url.QueryEscape(location),
	)

	req, err := http.NewRequestWithContext(ctx, "GET", geocodeURL, nil)
	if err != nil {
		return 0, 0, "", err
	}

	resp, err := wc.client.Do(req)
	if err != nil {
		return 0, 0, "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, "", err
	}

	var result struct {
		Results []struct {
			Name      string  `json:"name"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Country   string  `json:"country"`
			Admin1    string  `json:"admin1"` // state/province
			Timezone  string  `json:"timezone"`
		} `json:"results"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return 0, 0, "", fmt.Errorf("failed to parse geocoding response: %w", err)
	}

	if len(result.Results) == 0 {
		return 0, 0, "", fmt.Errorf("location not found: %s", location)
	}

	r := result.Results[0]
	displayName := r.Name
	if r.Admin1 != "" {
		displayName += ", " + r.Admin1
	}
	if r.Country != "" {
		displayName += ", " + r.Country
	}

	return r.Latitude, r.Longitude, displayName, nil
}

// fetchWeather fetches current + forecast weather from Open-Meteo
func (wc *WeatherClient) fetchWeather(ctx context.Context, lat, lon float64) (*WeatherData, error) {
	params := url.Values{}
	params.Set("latitude", fmt.Sprintf("%.4f", lat))
	params.Set("longitude", fmt.Sprintf("%.4f", lon))
	params.Set("current", "temperature_2m,relative_humidity_2m,apparent_temperature,is_day,weather_code,wind_speed_10m,wind_direction_10m")
	params.Set("hourly", "temperature_2m,relative_humidity_2m,precipitation_probability,weather_code,wind_speed_10m")
	params.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min,sunrise,sunset,precipitation_sum,precipitation_probability_max,wind_speed_10m_max")
	params.Set("temperature_unit", "fahrenheit")
	params.Set("wind_speed_unit", "mph")
	params.Set("precipitation_unit", "inch")
	params.Set("timezone", "auto")
	params.Set("forecast_days", "7")
	params.Set("forecast_hours", "24")

	weatherURL := "https://api.open-meteo.com/v1/forecast?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", weatherURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := wc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("weather request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Open-Meteo error (status %d): %s", resp.StatusCode, string(body))
	}

	var raw struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Timezone  string  `json:"timezone"`
		Current   struct {
			Temperature   float64 `json:"temperature_2m"`
			Humidity      int     `json:"relative_humidity_2m"`
			ApparentTemp  float64 `json:"apparent_temperature"`
			IsDay         int     `json:"is_day"`
			WeatherCode   int     `json:"weather_code"`
			WindSpeed     float64 `json:"wind_speed_10m"`
			WindDirection int     `json:"wind_direction_10m"`
		} `json:"current"`
		Hourly struct {
			Time        []string  `json:"time"`
			Temperature []float64 `json:"temperature_2m"`
			Humidity    []int     `json:"relative_humidity_2m"`
			PrecipProb  []int     `json:"precipitation_probability"`
			WeatherCode []int     `json:"weather_code"`
			WindSpeed   []float64 `json:"wind_speed_10m"`
		} `json:"hourly"`
		Daily struct {
			Time          []string  `json:"time"`
			WeatherCode   []int     `json:"weather_code"`
			TempMax       []float64 `json:"temperature_2m_max"`
			TempMin       []float64 `json:"temperature_2m_min"`
			Sunrise       []string  `json:"sunrise"`
			Sunset        []string  `json:"sunset"`
			PrecipSum     []float64 `json:"precipitation_sum"`
			PrecipProbMax []int     `json:"precipitation_probability_max"`
			WindSpeedMax  []float64 `json:"wind_speed_10m_max"`
		} `json:"daily"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse weather response: %w", err)
	}

	weather := &WeatherData{
		Latitude:  raw.Latitude,
		Longitude: raw.Longitude,
		Timezone:  raw.Timezone,
		Current: CurrentWeather{
			Temperature:   raw.Current.Temperature,
			FeelsLike:     raw.Current.ApparentTemp,
			Humidity:      raw.Current.Humidity,
			WindSpeed:     raw.Current.WindSpeed,
			WindDirection: raw.Current.WindDirection,
			Condition:     wmoCodeToCondition(raw.Current.WeatherCode),
			ConditionCode: raw.Current.WeatherCode,
			IsDay:         raw.Current.IsDay == 1,
		},
	}

	// Parse hourly (next 24 hours)
	for i := 0; i < len(raw.Hourly.Time) && i < 24; i++ {
		weather.HourlyNext = append(weather.HourlyNext, HourlyWeather{
			Time:          raw.Hourly.Time[i],
			Temperature:   raw.Hourly.Temperature[i],
			Humidity:      raw.Hourly.Humidity[i],
			RainChance:    raw.Hourly.PrecipProb[i],
			WindSpeed:     raw.Hourly.WindSpeed[i],
			Condition:     wmoCodeToCondition(raw.Hourly.WeatherCode[i]),
			ConditionCode: raw.Hourly.WeatherCode[i],
		})
	}

	// Parse daily (7 days)
	for i := 0; i < len(raw.Daily.Time); i++ {
		weather.DailyNext = append(weather.DailyNext, DailyWeather{
			Date:         raw.Daily.Time[i],
			TempMax:      raw.Daily.TempMax[i],
			TempMin:      raw.Daily.TempMin[i],
			RainChance:   raw.Daily.PrecipProbMax[i],
			RainSum:      raw.Daily.PrecipSum[i],
			WindSpeedMax: raw.Daily.WindSpeedMax[i],
			Condition:    wmoCodeToCondition(raw.Daily.WeatherCode[i]),
			Sunrise:      raw.Daily.Sunrise[i],
			Sunset:       raw.Daily.Sunset[i],
		})
	}

	return weather, nil
}

// FormatWeather formats weather data into readable output for piping
func FormatWeather(w *WeatherData) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Weather for %s\n\n", w.Location))

	// Current conditions
	sb.WriteString("## Current Conditions\n")
	sb.WriteString(fmt.Sprintf("- **Temperature:** %.0f°F (feels like %.0f°F)\n", w.Current.Temperature, w.Current.FeelsLike))
	sb.WriteString(fmt.Sprintf("- **Condition:** %s\n", w.Current.Condition))
	sb.WriteString(fmt.Sprintf("- **Humidity:** %d%%\n", w.Current.Humidity))
	sb.WriteString(fmt.Sprintf("- **Wind:** %.0f mph %s\n", w.Current.WindSpeed, degreeToDirection(w.Current.WindDirection)))
	sb.WriteString("\n")

	// Today's hourly (next 12 hours, every 3 hours)
	sb.WriteString("## Next 12 Hours\n")
	sb.WriteString("| Time | Temp | Condition | Rain% | Wind |\n")
	sb.WriteString("|------|------|-----------|-------|------|\n")
	for i := 0; i < len(w.HourlyNext) && i < 12; i += 1 {
		h := w.HourlyNext[i]
		// Parse and format time
		t, err := time.Parse("2006-01-02T15:04", h.Time)
		timeStr := h.Time
		if err == nil {
			timeStr = t.Format("3:04 PM")
		}
		sb.WriteString(fmt.Sprintf("| %s | %.0f°F | %s | %d%% | %.0f mph |\n",
			timeStr, h.Temperature, h.Condition, h.RainChance, h.WindSpeed))
	}
	sb.WriteString("\n")

	// 7-day forecast
	sb.WriteString("## 7-Day Forecast\n")
	sb.WriteString("| Date | High | Low | Condition | Rain% | Wind |\n")
	sb.WriteString("|------|------|-----|-----------|-------|------|\n")
	for _, d := range w.DailyNext {
		// Parse and format date
		t, err := time.Parse("2006-01-02", d.Date)
		dateStr := d.Date
		if err == nil {
			dateStr = t.Format("Mon Jan 2")
		}
		sb.WriteString(fmt.Sprintf("| %s | %.0f°F | %.0f°F | %s | %d%% | %.0f mph |\n",
			dateStr, d.TempMax, d.TempMin, d.Condition, d.RainChance, d.WindSpeedMax))
	}

	return sb.String()
}

// FormatWeatherBrief returns a one-line summary for piping
func FormatWeatherBrief(w *WeatherData) string {
	return fmt.Sprintf("%s: %.0f°F (feels like %.0f°F), %s, Humidity %d%%, Wind %.0f mph %s",
		w.Location,
		w.Current.Temperature,
		w.Current.FeelsLike,
		w.Current.Condition,
		w.Current.Humidity,
		w.Current.WindSpeed,
		degreeToDirection(w.Current.WindDirection),
	)
}

// wmoCodeToCondition converts WMO weather codes to human-readable conditions
// See: https://open-meteo.com/en/docs#weathervariables
func wmoCodeToCondition(code int) string {
	switch code {
	case 0:
		return "Clear sky"
	case 1:
		return "Mainly clear"
	case 2:
		return "Partly cloudy"
	case 3:
		return "Overcast"
	case 45:
		return "Fog"
	case 48:
		return "Depositing rime fog"
	case 51:
		return "Light drizzle"
	case 53:
		return "Moderate drizzle"
	case 55:
		return "Dense drizzle"
	case 56:
		return "Light freezing drizzle"
	case 57:
		return "Dense freezing drizzle"
	case 61:
		return "Slight rain"
	case 63:
		return "Moderate rain"
	case 65:
		return "Heavy rain"
	case 66:
		return "Light freezing rain"
	case 67:
		return "Heavy freezing rain"
	case 71:
		return "Slight snow"
	case 73:
		return "Moderate snow"
	case 75:
		return "Heavy snow"
	case 77:
		return "Snow grains"
	case 80:
		return "Slight rain showers"
	case 81:
		return "Moderate rain showers"
	case 82:
		return "Violent rain showers"
	case 85:
		return "Slight snow showers"
	case 86:
		return "Heavy snow showers"
	case 95:
		return "Thunderstorm"
	case 96:
		return "Thunderstorm with slight hail"
	case 99:
		return "Thunderstorm with heavy hail"
	default:
		return fmt.Sprintf("Unknown (%d)", code)
	}
}

// degreeToDirection converts wind degrees to cardinal direction
func degreeToDirection(deg int) string {
	directions := []string{"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE",
		"S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}
	idx := ((deg + 11) / 22) % 16
	return directions[idx]
}
