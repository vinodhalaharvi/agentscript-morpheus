package agentscript

// registry.go is the single place that knows about all plugins.
// It imports every pkg/* plugin and wires them into the Registry.
//
// Adding a new package = one new import + one new Register() call here.
// Nothing else changes — not runtime.go, not the switch, not the grammar.
//
// This file is the composition root for the plugin system.

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/vinodhalaharvi/agentscript/pkg/agent"
	"github.com/vinodhalaharvi/agentscript/pkg/cache"
	"github.com/vinodhalaharvi/agentscript/pkg/cloudrun"
	agcrypto "github.com/vinodhalaharvi/agentscript/pkg/crypto"
	"github.com/vinodhalaharvi/agentscript/pkg/datatable"
	aggithub "github.com/vinodhalaharvi/agentscript/pkg/github"
	"github.com/vinodhalaharvi/agentscript/pkg/huggingface"
	"github.com/vinodhalaharvi/agentscript/pkg/jobsearch"
	"github.com/vinodhalaharvi/agentscript/pkg/mcp"
	"github.com/vinodhalaharvi/agentscript/pkg/mcpagent"
	"github.com/vinodhalaharvi/agentscript/pkg/mcpsearch"
	"github.com/vinodhalaharvi/agentscript/pkg/network"
	"github.com/vinodhalaharvi/agentscript/pkg/news"
	"github.com/vinodhalaharvi/agentscript/pkg/notify"
	"github.com/vinodhalaharvi/agentscript/pkg/openai"
	"github.com/vinodhalaharvi/agentscript/pkg/pdffill"
	"github.com/vinodhalaharvi/agentscript/pkg/perplexity"
	"github.com/vinodhalaharvi/agentscript/pkg/plugagent"
	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
	"github.com/vinodhalaharvi/agentscript/pkg/reddit"
	"github.com/vinodhalaharvi/agentscript/pkg/review"
	"github.com/vinodhalaharvi/agentscript/pkg/rss"
	agstock "github.com/vinodhalaharvi/agentscript/pkg/stock"
	"github.com/vinodhalaharvi/agentscript/pkg/twitter"
	"github.com/vinodhalaharvi/agentscript/pkg/weather"
	"github.com/vinodhalaharvi/agentscript/pkg/whatsapp"
)

// buildRegistry constructs the plugin registry from the runtime's clients.
// Called once from NewRuntime — the result is stored on r.registry.
func (r *Runtime) buildRegistry(c *cache.Cache) *plugin.Registry {
	reg := plugin.NewRegistry()

	// --- Data plugins ---
	reg.Register(weather.NewPlugin(c, r.verbose))
	reg.Register(agstock.NewPlugin(r.searchKey, c, r.verbose))
	reg.Register(agcrypto.NewPlugin(c, r.verbose))
	reg.Register(news.NewPlugin(r.searchKey, c, r.verbose))
	reg.Register(reddit.NewPlugin(c, r.verbose))
	reg.Register(rss.NewPlugin(c, r.verbose))
	reg.Register(twitter.NewPlugin(r.verbose))
	reg.Register(jobsearch.NewPlugin(r.searchKey, c, r.verbose))

	// --- Notification plugins ---
	reg.Register(notify.NewPlugin(r.verbose))
	reg.Register(whatsapp.NewPlugin(r.verbose))

	// --- HuggingFace ---
	reg.Register(huggingface.NewPlugin(r.verbose))

	// --- Perplexity AI Search ---
	// API key from PERPLEXITY_API_KEY. Falls back gracefully if not set.
	if pplxKey := os.Getenv("PERPLEXITY_API_KEY"); pplxKey != "" {
		reg.Register(perplexity.NewPlugin(pplxKey, "", r.verbose))
	}

	// --- MCP — stateful, shares the same client as the runtime ---
	reg.Register(mcp.NewPlugin(r.mcp))

	// --- MCP Agent — AI-driven tool selection over connected MCP servers
	// Reasoner is Claude if available, Gemini as fallback.
	// Same MCPClient as above — shares already-connected servers.
	var mcpReasoner mcpagent.Reasoner
	if r.claude != nil {
		mcpReasoner = r.claude.Chat
	} else if r.gemini != nil {
		mcpReasoner = r.gemini.GenerateContent
	}
	if mcpReasoner != nil {
		reg.Register(mcpagent.NewPlugin(r.mcp, mcpReasoner, r.verbose))
	}

	// --- Agent — natural language to DSL via Claude
	// r.RunDSL is the Executor seam — same pattern as ReactGenerator.
	if r.claude != nil {
		reg.Register(agent.NewPlugin(r.claude, r.RunDSL, r.verbose))
	}

	// --- Network diagnostics — pure Go, no external deps ---
	reg.Register(network.NewPlugin(r.verbose))

	// --- PDF Form Fill — AI-powered PDF form filling ---
	// Reasoner is Claude if available, Gemini as fallback.
	var pdfReasoner pdffill.Reasoner
	if r.claude != nil {
		pdfReasoner = r.claude.Chat
	} else if r.gemini != nil {
		pdfReasoner = r.gemini.GenerateContent
	}
	reg.Register(pdffill.NewPlugin(pdfReasoner, r.verbose))

	// --- Cloud Run — deploy + schedule DSL scripts as Cloud Run Jobs ---
	reg.Register(cloudrun.NewPlugin(r.verbose))

	// --- DataTable — render .table DSL into self-contained HTML ---
	reg.Register(datatable.NewPlugin(r.verbose))

	// --- MCP Search — searches the official MCP registry
	// No API key needed — the registry is public
	reg.Register(mcpsearch.NewPlugin(r.verbose))

	// --- Plug Agent — generates new plugins from English descriptions
	// Generator is Claude.Chat — injected as functional field seam.
	// repoRoot is detected from the binary's working directory.
	if r.claude != nil {
		repoRoot, _ := os.Getwd()
		reg.Register(plugagent.NewPlugin(r.claude.Chat, repoRoot, r.verbose))
	}

	// --- Code Review Forum — Claude + Gemini + GPT-4 debate
	// GeminiReviewer is the functional field seam for Gemini injection.
	// Gracefully degrades — works with any subset of the three models.
	var geminiReviewer review.GeminiReviewer
	if r.gemini != nil {
		geminiReviewer = r.gemini.GenerateContent
	}
	var openaiClient *openai.Client
	if oaiKey := os.Getenv("OPENAI_API_KEY"); oaiKey != "" {
		openaiClient = openai.NewClient(oaiKey, "")
	}
	if r.claude != nil || geminiReviewer != nil || openaiClient != nil {
		reg.Register(review.NewPlugin(r.claude, geminiReviewer, openaiClient, r.verbose))
	}

	// --- GitHub — ReactGenerator is the functional field seam.
	// Claude is preferred; Gemini is the fallback; nil means no AI available.
	// The github plugin doesn't know which it gets — just that it's a function.
	var reactGen aggithub.ReactGenerator
	if r.claude != nil {
		reactGen = r.claude.GenerateReactSPA
	} else if r.gemini != nil {
		reactGen = r.buildGeminiReactGenerator()
	}
	reg.Register(aggithub.NewPlugin(r.github, reactGen))

	return reg
}

// buildGeminiReactGenerator adapts the Gemini client into a ReactGenerator.
// This is the XxxFunc bridge pattern — Gemini's GenerateContent doesn't
// match ReactGenerator's signature, so we wrap it in a closure.
func (r *Runtime) buildGeminiReactGenerator() aggithub.ReactGenerator {
	return func(ctx context.Context, title, content string) (string, error) {
		prompt := buildReactSPAPrompt(title, content)
		result, err := r.gemini.GenerateContent(ctx, prompt)
		if err != nil {
			return "", err
		}
		return cleanHTMLResponse(result), nil
	}
}

// buildReactSPAPrompt constructs the prompt for React SPA generation.
// Extracted here so it can be used by both Gemini and any future generator.
func buildReactSPAPrompt(title, content string) string {
	return fmt.Sprintf(`Generate a beautiful, modern React single-page application (SPA) for the following content.

TITLE: %s

CONTENT:
%s

REQUIREMENTS:
1. Output ONLY the complete HTML file with embedded React (using babel standalone)
2. Use React hooks (useState, useEffect)
3. Modern, dark theme UI with gradients and animations
4. Responsive design with Tailwind CSS (via CDN)
5. Include smooth scroll animations
6. Add a navigation header if content has sections
7. Use React icons or emojis for visual appeal
8. Make it visually stunning - this is for a hackathon demo!
9. Include a footer crediting "Built with AgentScript"

OUTPUT FORMAT:
Return ONLY the HTML code starting with <!DOCTYPE html> and ending with </html>
No markdown, no explanation, just the raw HTML/React code.`, title, content)
}

// cleanHTMLResponse strips markdown code fences from an AI-generated HTML response.
func cleanHTMLResponse(result string) string {
	result = strings.TrimSpace(result)
	result = strings.TrimPrefix(result, "```html")
	result = strings.TrimPrefix(result, "```")
	result = strings.TrimSuffix(result, "```")
	result = strings.TrimSpace(result)

	if !strings.HasPrefix(result, "<!DOCTYPE html>") && !strings.HasPrefix(result, "<html") {
		if idx := strings.Index(result, "<!DOCTYPE html>"); idx != -1 {
			return result[idx:]
		}
		if idx := strings.Index(result, "<html"); idx != -1 {
			return result[idx:]
		}
	}
	return result
}
