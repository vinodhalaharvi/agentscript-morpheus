package main

import (
	"strings"
	"testing"
)

// ── stripMarkdown ─────────────────────────────────────────────────────────────

func TestStripMarkdown_RemovesHeaders(t *testing.T) {
	out := stripMarkdown("# Heading\n## Sub")
	if strings.Contains(out, "#") {
		t.Errorf("headers not removed: %q", out)
	}
}

func TestStripMarkdown_RemovesBold(t *testing.T) {
	out := stripMarkdown("**Temperature:** 70°F")
	if strings.Contains(out, "**") {
		t.Errorf("bold not removed: %q", out)
	}
	if !strings.Contains(out, "70°F") {
		t.Errorf("content removed: %q", out)
	}
}

func TestStripMarkdown_RemovesTableSeparator(t *testing.T) {
	out := stripMarkdown("|------|------|\n| 12pm | 70°F |")
	if strings.Contains(out, "|---") {
		t.Errorf("table separator not removed: %q", out)
	}
	if !strings.Contains(out, "70°F") {
		t.Errorf("content removed: %q", out)
	}
}

func TestStripMarkdown_RemovesTablePipes(t *testing.T) {
	out := stripMarkdown("| Time | Temp |")
	if strings.Contains(out, "|") {
		t.Errorf("pipes not removed: %q", out)
	}
}

func TestStripMarkdown_KeepsPlainText(t *testing.T) {
	input := "Temperature is 70°F. Wind 6 mph from SW."
	out := stripMarkdown(input)
	if !strings.Contains(out, "70°F") || !strings.Contains(out, "Wind") {
		t.Errorf("plain text was mangled: %q", out)
	}
}

func TestStripMarkdown_FullWeatherPayload(t *testing.T) {
	input := `# Weather for New York
## Current Conditions
- **Temperature:** 70°F (feels like 70°F)
- **Condition:** Clear sky
- **Humidity:** 49%
| Time | Temp | Condition |
|------|------|-----------|
| 12pm | 70°F | Clear sky |`

	out := stripMarkdown(input)
	for _, bad := range []string{"**", "|---", "# "} {
		if strings.Contains(out, bad) {
			t.Errorf("output still contains %q:\n%s", bad, out)
		}
	}
	for _, want := range []string{"70°F", "Clear sky", "49%"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// ── extractFirstArg ───────────────────────────────────────────────────────────

func TestExtractFirstArg(t *testing.T) {
	cases := []struct{ input, want string }{
		{`job_search "golang developer" "remote" ""`, "golang developer"},
		{`weather "New York"`, "New York"},
		{`search "AI news today"`, "AI news today"},
		{`no quotes here`, `no quotes here`},
		{`crypto "BTC"`, "BTC"},
	}
	for _, c := range cases {
		got := extractFirstArg(c.input)
		if got != c.want {
			t.Errorf("extractFirstArg(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// ── buildSystemPrompt ─────────────────────────────────────────────────────────

func TestBuildSystemPrompt_ContainsBase(t *testing.T) {
	p := buildSystemPrompt()
	if !strings.Contains(p, "agentscript_dsl") {
		t.Error("prompt missing agentscript_dsl reference")
	}
	if !strings.Contains(p, "weather") {
		t.Error("prompt missing weather command")
	}
}

func TestBuildSystemPrompt_InjectsEmail(t *testing.T) {
	t.Setenv("USER_EMAIL", "test@example.com")
	p := buildSystemPrompt()
	if !strings.Contains(p, "test@example.com") {
		t.Error("prompt missing USER_EMAIL")
	}
}

func TestBuildSystemPrompt_InjectsName(t *testing.T) {
	t.Setenv("USER_NAME", "Vinod")
	p := buildSystemPrompt()
	if !strings.Contains(p, "Vinod") {
		t.Error("prompt missing USER_NAME")
	}
}

// ── DSL grammar examples ──────────────────────────────────────────────────────
// These just verify our documented examples parse cleanly as strings.
// No runtime needed — just sanity-check the format.

func TestDSLExamples_NoDotsOrParens(t *testing.T) {
	examples := []string{
		`weather "New York"`,
		`crypto "BTC"`,
		`job_search "golang developer" "remote" "fulltime"`,
		`( weather "NYC" <*> crypto "BTC" ) >=> merge`,
		`news "AI" >=> summarize`,
		`search "score" >=> email "user@example.com"`,
	}
	for _, ex := range examples {
		// Ensure no dot-notation or function-call syntax leaked in
		if strings.Contains(ex, ".") && !strings.Contains(ex, "@") {
			t.Errorf("example contains dot notation: %s", ex)
		}
		if strings.Contains(ex, "(\"") || strings.Contains(ex, "=\"") {
			t.Errorf("example contains paren/equals syntax: %s", ex)
		}
	}
}
