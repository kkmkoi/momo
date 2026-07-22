package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// wttrResponse represents the JSON response from wttr.in.
type wttrResponse struct {
	CurrentCondition []struct {
		TempC       string `json:"temp_C"`
		Humidity    string `json:"humidity"`
		WeatherDesc []struct {
			Value string `json:"value"`
		} `json:"weatherDesc"`
		WindSpeedKmph  string `json:"windspeedKmph"`
		WindDir16Point string `json:"winddir16Point"`
		FeelsLikeC     string `json:"FeelsLikeC"`
	} `json:"current_condition"`
	NearestArea []struct {
		AreaName []struct {
			Value string `json:"value"`
		} `json:"areaName"`
		Region []struct {
			Value string `json:"value"`
		} `json:"region"`
		Country []struct {
			Value string `json:"value"`
		} `json:"country"`
	} `json:"nearest_area"`
}

// Weather is a real-time weather tool powered by wttr.in (free, no API key required).
type Weather struct {
	client *http.Client
}

// NewWeather creates a weather tool that fetches live data from wttr.in.
func NewWeather() *Weather {
	return &Weather{
		client: newHTTPClient(),
	}
}

func (w *Weather) Name() string { return "weather" }

func (w *Weather) Description() string {
	return "Get the current weather for any city worldwide. Supports city names in any language."
}

func (w *Weather) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"city": map[string]any{
				"type":        "string",
				"description": "The city name to get weather for, e.g. 'Beijing', 'Tokyo', 'London', 'New York', '成都'",
			},
		},
		"required": []string{"city"},
	}
}

func (w *Weather) Execute(_ context.Context, params map[string]any) (string, error) {
	city, _ := params["city"].(string)
	city = strings.TrimSpace(city)
	if city == "" {
		return "", fmt.Errorf("city is required")
	}

	// Fetch real-time weather from wttr.in.
	encoded := url.QueryEscape(city)
	apiURL := fmt.Sprintf("https://wttr.in/%s?format=j1", encoded)

	resp, err := w.client.Get(apiURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch weather data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("weather API returned status %d", resp.StatusCode)
	}

	var data wttrResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("failed to parse weather data: %w", err)
	}

	if len(data.CurrentCondition) == 0 {
		return fmt.Sprintf("No weather data available for %q.", city), nil
	}

	cc := data.CurrentCondition[0]

	// Build city display name.
	cityDisplay := city
	countryDisplay := ""
	if len(data.NearestArea) > 0 {
		area := data.NearestArea[0]
		if len(area.AreaName) > 0 {
			cityDisplay = area.AreaName[0].Value
		}
		if len(area.Country) > 0 {
			countryDisplay = area.Country[0].Value
		}
	}

	weatherDesc := "N/A"
	if len(cc.WeatherDesc) > 0 {
		weatherDesc = cc.WeatherDesc[0].Value
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("🌤️  %s", cityDisplay))
	if countryDisplay != "" {
		b.WriteString(fmt.Sprintf(", %s", countryDisplay))
	}
	b.WriteString(" — Current Weather\n")
	b.WriteString(fmt.Sprintf("🌡️  Temperature: %s°C (feels like %s°C)\n", cc.TempC, cc.FeelsLikeC))
	b.WriteString(fmt.Sprintf("☁️  Condition: %s\n", weatherDesc))
	b.WriteString(fmt.Sprintf("💧 Humidity: %s%%\n", cc.Humidity))
	b.WriteString(fmt.Sprintf("💨 Wind: %s km/h %s\n", cc.WindSpeedKmph, cc.WindDir16Point))

	return b.String(), nil
}
