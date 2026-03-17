package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// ReactGenerator is the seam between the github plugin and whichever AI
// generates the React SPA. The plugin doesn't know if it's Claude or Gemini —
// it only knows that something can turn (title, content) into HTML.
// This is the functional field pattern applied exactly where the domain says
// there is genuine variation: different AI backends.
type ReactGenerator func(ctx context.Context, title, content string) (string, error)

// Plugin wraps GitHubClient as a Plugin.
// It accepts an optional ReactGenerator — if nil, github_pages is unavailable
// but github_pages_html still works.
type Plugin struct {
	client    *GitHubClient
	generator ReactGenerator // optional — nil means no AI available
}

// NewPlugin creates a github plugin.
// generator may be nil if no AI API key is configured.
func NewPlugin(client *GitHubClient, generator ReactGenerator) *Plugin {
	return &Plugin{client: client, generator: generator}
}

func (p *Plugin) Name() string { return "github" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"github_pages":      p.githubPages,
		"github_pages_html": p.githubPagesHTML,
	}
}

// githubPages generates a React SPA via AI and deploys it to GitHub Pages.
func (p *Plugin) githubPages(ctx context.Context, args []string, input string) (string, error) {
	if p.client == nil {
		return "", githubNotConfiguredErr()
	}
	if input == "" {
		return "", fmt.Errorf("no content to deploy — pipe content into github_pages")
	}
	title := plugin.Arg(args, 0)
	if title == "" {
		title = "AgentScript Page"
	}
	if p.generator == nil {
		return "", fmt.Errorf(
			"no AI API key configured for React SPA generation\n" +
				"Set CLAUDE_API_KEY or GEMINI_API_KEY, or use github_pages_html for plain HTML",
		)
	}

	fmt.Printf("🎨 Generating React SPA...\n")
	reactCode, err := p.generator(ctx, title, input)
	if err != nil {
		return "", fmt.Errorf("React SPA generation failed: %w", err)
	}

	repoName := sanitizeRepoName(title)
	fmt.Printf("🚀 Deploying to GitHub Pages: %s...\n", title)
	pagesURL, err := p.client.DeployReactSPA(ctx, repoName, title, reactCode)
	if err != nil {
		return "", fmt.Errorf("GitHub Pages deployment failed: %w", err)
	}
	fmt.Printf("✅ Deployed to: %s\n   (Note: May take 1-2 minutes to go live)\n", pagesURL)
	return pagesURL, nil
}

// githubPagesHTML deploys raw HTML directly to GitHub Pages — no AI needed.
func (p *Plugin) githubPagesHTML(ctx context.Context, args []string, input string) (string, error) {
	if p.client == nil {
		return "", githubNotConfiguredErr()
	}
	if input == "" {
		return "", fmt.Errorf("no content to deploy — pipe content into github_pages_html")
	}
	title := plugin.Arg(args, 0)
	if title == "" {
		title = "AgentScript Page"
	}
	repoName := sanitizeRepoName(title)
	fmt.Printf("🚀 Deploying HTML to GitHub Pages: %s...\n", title)
	pagesURL, err := p.client.DeployToPages(ctx, repoName, title, input)
	if err != nil {
		return "", fmt.Errorf("GitHub Pages deployment failed: %w", err)
	}
	fmt.Printf("✅ Deployed to: %s\n   (Note: May take 1-2 minutes to go live)\n", pagesURL)
	return pagesURL, nil
}

func sanitizeRepoName(title string) string {
	name := strings.ToLower(title)
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, "'", "")
	name = strings.ReplaceAll(name, "\"", "")
	return name
}

func githubNotConfiguredErr() error {
	return fmt.Errorf(
		"GitHub API not configured\n" +
			"Setup: https://github.com/settings/developers → New OAuth App\n" +
			"Then set GITHUB_CLIENT_ID and GITHUB_CLIENT_SECRET",
	)
}
