package weather

import (
	"context"
	"fmt"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/cache"
	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Plugin wraps WeatherClient as a Plugin.
type Plugin struct {
	client  *WeatherClient
	cache   *cache.Cache
	verbose bool
}

// NewPlugin creates a weather plugin.
func NewPlugin(c *cache.Cache, verbose bool) *Plugin {
	return &Plugin{
		client:  NewWeatherClient(verbose),
		cache:   c,
		verbose: verbose,
	}
}

func (p *Plugin) Name() string { return "weather" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"weather": p.weather,
	}
}

func (p *Plugin) weather(ctx context.Context, args []string, input string) (string, error) {
	location := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if location == "" {
		return "", fmt.Errorf("weather requires a location")
	}
	return cache.CachedGet(p.cache, "weather", location, cache.CacheTTLWeather, func() (string, error) {
		data, err := p.client.GetWeather(ctx, location)
		if err != nil {
			return "", err
		}
		return FormatWeather(data), nil
	})
}
