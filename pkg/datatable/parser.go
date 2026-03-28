// Package main implements the datatable-gen compiler.
// It parses a minimal .table DSL and generates a self-contained
// HTML file with React DataTable, supporting 4 data sources:
//   - static: embedded JSON
//   - rest:   fetch() with optional polling
//   - sse:    EventSource (server-sent events)
//   - ws:     WebSocket
package datatable

import (
	"fmt"
	"os"
	"strings"
	"unicode"
)

// SourceType defines how data is loaded.
type SourceType string

const (
	SourceStatic SourceType = "static" // embedded JSON data
	SourceRest   SourceType = "rest"   // fetch() + optional poll
	SourceSSE    SourceType = "sse"    // EventSource
	SourceWS     SourceType = "ws"     // WebSocket
)

// ColumnType defines how a column is rendered.
type ColumnType string

const (
	ColText   ColumnType = "text"
	ColNumber ColumnType = "number"
	ColBadge  ColumnType = "badge"
	ColDate   ColumnType = "date"
	ColLink   ColumnType = "link"
)

// Source defines the data source for the table.
type Source struct {
	Type    SourceType
	URL     string // file path or URL
	PollSec int    // polling interval in seconds (rest only)
}

// Column defines a single table column.
type Column struct {
	Field      string     // JSON field name
	Label      string     // display header
	Type       ColumnType // render type
	Sortable   bool
	Filterable bool
	Color      string // color scheme: "health", "status", "latency"
	Range      bool   // enable range filter for numbers
}

// TableDef is the parsed representation of a .table file.
type TableDef struct {
	Title     string
	Source    Source
	Columns   []Column
	Theme     string // "dark" or "light"
	Search    bool
	PageSize  int
	ExportCSV bool
	DataField string // if JSON has a wrapper field e.g. "sites"
}

// Parser holds tokenizer state.
type Parser struct {
	lines []string
	pos   int
}

// NewParser creates a parser from DSL source text.
func NewParser(src string) *Parser {
	var lines []string
	for _, l := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		lines = append(lines, trimmed)
	}
	return &Parser{lines: lines}
}

// Parse parses the full .table DSL into a TableDef.
func (p *Parser) Parse() (*TableDef, error) {
	td := &TableDef{
		Theme:    "dark",
		Search:   true,
		PageSize: 25,
	}

	for p.pos < len(p.lines) {
		line := p.lines[p.pos]

		// table "Title" {
		if strings.HasPrefix(line, "table ") {
			td.Title = extractString(line)
			p.pos++
			continue
		}

		// source <type> <url> [poll=Ns] [field=<name>]
		if strings.HasPrefix(line, "source ") {
			src, dataField, err := parseSource(line)
			if err != nil {
				return nil, err
			}
			td.Source = src
			if dataField != "" {
				td.DataField = dataField
			}
			p.pos++
			continue
		}

		// columns { ... }
		if line == "columns {" {
			p.pos++
			cols, err := p.parseColumns()
			if err != nil {
				return nil, err
			}
			td.Columns = cols
			continue
		}

		// theme dark|light
		if strings.HasPrefix(line, "theme ") {
			td.Theme = strings.TrimPrefix(line, "theme ")
			p.pos++
			continue
		}

		// search true|false
		if strings.HasPrefix(line, "search ") {
			td.Search = strings.TrimPrefix(line, "search ") == "true"
			p.pos++
			continue
		}

		// pagination <N>
		if strings.HasPrefix(line, "pagination ") {
			fmt.Sscanf(strings.TrimPrefix(line, "pagination "), "%d", &td.PageSize)
			p.pos++
			continue
		}

		// export csv
		if line == "export csv" {
			td.ExportCSV = true
			p.pos++
			continue
		}

		// field <name>  — top-level data field wrapper
		if strings.HasPrefix(line, "field ") {
			td.DataField = strings.TrimPrefix(line, "field ")
			p.pos++
			continue
		}

		// skip closing braces and unknown lines
		p.pos++
	}

	if td.Title == "" {
		td.Title = "Dashboard"
	}
	if len(td.Columns) == 0 {
		return nil, fmt.Errorf("no columns defined")
	}

	return td, nil
}

// parseColumns parses the columns { ... } block.
func (p *Parser) parseColumns() ([]Column, error) {
	var cols []Column
	for p.pos < len(p.lines) {
		line := p.lines[p.pos]
		if line == "}" {
			p.pos++
			break
		}
		col, err := parseColumn(line)
		if err != nil {
			return nil, fmt.Errorf("column parse error: %w (line: %q)", err, line)
		}
		cols = append(cols, col)
		p.pos++
	}
	return cols, nil
}

// parseColumn parses one column definition line.
// Format: field "Label" type [flags...]
// Example: ssl_status "SSL Status" badge sortable filterable color=health
func parseColumn(line string) (Column, error) {
	tokens := tokenize(line)
	if len(tokens) < 3 {
		return Column{}, fmt.Errorf("expected: field \"Label\" type [flags]")
	}

	col := Column{
		Field: tokens[0],
		Label: tokens[1],
		Type:  ColText,
	}

	// parse type
	switch tokens[2] {
	case "text":
		col.Type = ColText
	case "number":
		col.Type = ColNumber
	case "badge":
		col.Type = ColBadge
	case "date":
		col.Type = ColDate
	case "link":
		col.Type = ColLink
	default:
		col.Type = ColText
	}

	// parse flags
	for _, tok := range tokens[3:] {
		switch {
		case tok == "sortable":
			col.Sortable = true
		case tok == "filterable":
			col.Filterable = true
		case tok == "range":
			col.Range = true
		case strings.HasPrefix(tok, "color="):
			col.Color = strings.TrimPrefix(tok, "color=")
		}
	}

	return col, nil
}

// parseSource parses: source <type> <url> [poll=Ns] [field=<name>]
func parseSource(line string) (Source, string, error) {
	tokens := tokenize(strings.TrimPrefix(line, "source "))
	if len(tokens) < 1 {
		return Source{}, "", fmt.Errorf("source needs a type: source static|rest|sse|ws")
	}

	src := Source{}
	dataField := ""

	switch tokens[0] {
	case "static":
		src.Type = SourceStatic
	case "rest":
		src.Type = SourceRest
	case "sse":
		src.Type = SourceSSE
	case "ws":
		src.Type = SourceWS
	default:
		return Source{}, "", fmt.Errorf("unknown source type %q — use: static rest sse ws", tokens[0])
	}

	// For non-static sources, URL is required
	if src.Type != SourceStatic && len(tokens) < 2 {
		return Source{}, "", fmt.Errorf("source %s needs a url: source %s \"url\"", tokens[0], tokens[0])
	}

	if len(tokens) >= 2 {
		src.URL = tokens[1]
	}

	if len(tokens) > 2 {
		for _, tok := range tokens[2:] {
			if strings.HasPrefix(tok, "poll=") {
				val := strings.TrimPrefix(tok, "poll=")
				val = strings.TrimSuffix(val, "s")
				fmt.Sscanf(val, "%d", &src.PollSec)
			}
			if strings.HasPrefix(tok, "field=") {
				dataField = strings.TrimPrefix(tok, "field=")
			}
		}
	}

	return src, dataField, nil
}

// extractString extracts the first quoted string from a line.
func extractString(line string) string {
	start := strings.Index(line, `"`)
	if start < 0 {
		return ""
	}
	end := strings.Index(line[start+1:], `"`)
	if end < 0 {
		return ""
	}
	return line[start+1 : start+1+end]
}

// tokenize splits a line into tokens, respecting quoted strings.
func tokenize(line string) []string {
	var tokens []string
	var cur strings.Builder
	inQuote := false

	for _, ch := range line {
		switch {
		case ch == '"':
			inQuote = !inQuote
			if !inQuote && cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		case unicode.IsSpace(ch) && !inQuote:
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(ch)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

// ParseFile reads and parses a .table DSL file.
func ParseFile(path string) (*TableDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	p := NewParser(string(data))
	return p.Parse()
}
