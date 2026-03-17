package agentscript

import (
	"context"
	"fmt"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/cache"
	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
	"github.com/vinodhalaharvi/agentscript/pkg/weather"
)

// WeatherPlugin handles the "weather" command.
type WeatherPlugin struct {
	cache   *cache.Cache
	verbose bool
}

func NewWeatherPlugin(c *cache.Cache, verbose bool) *WeatherPlugin {
	return &WeatherPlugin{cache: c, verbose: verbose}
}

func (p *WeatherPlugin) Name() string { return "weather" }

func (p *WeatherPlugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"weather": p.fetch,
	}
}

func (p *WeatherPlugin) fetch(ctx context.Context, args []string, input string) (string, error) {
	location := firstArg(args)
	if location == "" {
		location = strings.TrimSpace(input)
	}
	if location == "" {
		return "", fmt.Errorf("weather requires a location")
	}
	client := weather.NewWeatherClient(p.verbose)
	return cache.CachedGet(p.cache, "weather", location, cache.CacheTTLWeather, func() (string, error) {
		data, err := client.GetWeather(ctx, location)
		if err != nil {
			return "", err
		}
		return weather.FormatWeather(data), nil
	})
}
