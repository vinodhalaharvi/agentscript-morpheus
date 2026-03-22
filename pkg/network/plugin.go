package network

import (
	"context"
	"fmt"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Plugin exposes network diagnostic commands to the AgentScript DSL.
type Plugin struct {
	client *Client
}

// NewPlugin creates a network plugin.
func NewPlugin(verbose bool) *Plugin {
	return &Plugin{client: NewClient(verbose)}
}

func (p *Plugin) Name() string { return "network" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"ssl_check":  p.sslCheck,
		"ping":       p.ping,
		"dns_lookup": p.dnsLookup,
		"port_check": p.portCheck,
		"http_check": p.httpCheck,
		"whois":      p.whois,
	}
}

func (p *Plugin) sslCheck(ctx context.Context, args []string, input string) (string, error) {
	host := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if host == "" {
		return "", fmt.Errorf("ssl_check requires a hostname")
	}
	return p.client.SSLCheck(host)
}

func (p *Plugin) ping(ctx context.Context, args []string, input string) (string, error) {
	host := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if host == "" {
		return "", fmt.Errorf("ping requires a hostname")
	}
	return p.client.Ping(host)
}

func (p *Plugin) dnsLookup(ctx context.Context, args []string, input string) (string, error) {
	host := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if host == "" {
		return "", fmt.Errorf("dns_lookup requires a hostname")
	}
	return p.client.DNSLookup(host)
}

func (p *Plugin) portCheck(ctx context.Context, args []string, input string) (string, error) {
	host := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if host == "" {
		return "", fmt.Errorf("port_check requires a hostname")
	}
	ports := plugin.Arg(args, 1)
	if ports == "" {
		ports = "80,443,22,8080"
	}
	return p.client.PortCheck(host, ports)
}

func (p *Plugin) httpCheck(ctx context.Context, args []string, input string) (string, error) {
	url := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if url == "" {
		return "", fmt.Errorf("http_check requires a URL")
	}
	return p.client.HTTPCheck(url)
}

func (p *Plugin) whois(ctx context.Context, args []string, input string) (string, error) {
	domain := plugin.Coalesce(args, 0, strings.TrimSpace(input))
	if domain == "" {
		return "", fmt.Errorf("whois requires a domain")
	}
	return p.client.Whois(domain)
}
