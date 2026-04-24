package matching

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Parse compiles a pattern from source text. Supported grammar:
//
//	pattern    := simple ("when" <passthrough>)?
//	simple     := literal | wildcard | variable | object | list
//	literal    := string | number | bool | null
//	wildcard   := "_"
//	variable   := "$" identifier
//	object     := "{" field ("," field)* "}"
//	field      := identifier ":" pattern        // explicit
//	            | identifier                    // shorthand for `ident: $ident`
//	list       := "[" pattern ("," pattern)* ("," "..." variable)? "]"
//	            | "[" "..." variable "]"
//
// Guards are parsed but EvalFunc is left nil — callers plug in an evaluator.
// This keeps the parser decoupled from any expression language choice.
func Parse(src string) (Pattern, error) {
	src = strings.TrimSpace(src)
	// Strip guard if present
	var guardSrc string
	if idx := findGuardKeyword(src); idx >= 0 {
		guardSrc = strings.TrimSpace(src[idx+len(" when "):])
		src = strings.TrimSpace(src[:idx])
	}

	p := &parser{src: src, pos: 0}
	pat, err := p.parsePattern()
	if err != nil {
		return Pattern{}, err
	}
	p.skipWS()
	if p.pos != len(p.src) {
		return Pattern{}, fmt.Errorf("unexpected trailing input at %d: %q", p.pos, p.src[p.pos:])
	}
	if guardSrc != "" {
		pat.Guard = &Guard{Source: guardSrc}
	}
	return pat, nil
}

// findGuardKeyword finds " when " at the top level (not inside strings or braces).
// Returns the index of the space before "when", or -1 if no guard.
func findGuardKeyword(s string) int {
	depth := 0
	inStr := false
	i := 0
	for i < len(s) {
		c := s[i]
		if !inStr {
			if c == '"' {
				inStr = true
			} else if c == '{' || c == '[' {
				depth++
			} else if c == '}' || c == ']' {
				depth--
			} else if depth == 0 && i+6 <= len(s) && s[i:i+6] == " when " {
				return i
			}
		} else {
			if c == '\\' && i+1 < len(s) {
				i += 2
				continue
			}
			if c == '"' {
				inStr = false
			}
		}
		i++
	}
	return -1
}

type parser struct {
	src string
	pos int
}

func (p *parser) parsePattern() (Pattern, error) {
	p.skipWS()
	if p.pos >= len(p.src) {
		return Pattern{}, fmt.Errorf("unexpected end of input")
	}
	c := p.src[p.pos]
	switch {
	case c == '_':
		p.pos++
		// Guard: ensure it was standalone, not start of an identifier
		if p.pos < len(p.src) && (isIdentRune(rune(p.src[p.pos])) || p.src[p.pos] == '_') {
			return Pattern{}, fmt.Errorf("'_' must be standalone wildcard at pos %d", p.pos-1)
		}
		return Pattern{Wildcard: &WildcardPat{}}, nil
	case c == '$':
		return p.parseVariable()
	case c == '{':
		return p.parseObject()
	case c == '[':
		return p.parseList()
	case c == '"':
		return p.parseString()
	case c == '-' || (c >= '0' && c <= '9'):
		return p.parseNumber()
	case isIdentStart(rune(c)):
		// Could be true/false/null or a bare identifier (not legal here)
		return p.parseIdentLiteral()
	}
	return Pattern{}, fmt.Errorf("unexpected char %q at pos %d", c, p.pos)
}

func (p *parser) parseVariable() (Pattern, error) {
	if p.src[p.pos] != '$' {
		return Pattern{}, fmt.Errorf("expected '$' at pos %d", p.pos)
	}
	p.pos++
	name := p.readIdent()
	if name == "" {
		return Pattern{}, fmt.Errorf("expected identifier after '$' at pos %d", p.pos)
	}
	return Pattern{Variable: &VariablePat{Name: name}}, nil
}

func (p *parser) parseObject() (Pattern, error) {
	if p.src[p.pos] != '{' {
		return Pattern{}, fmt.Errorf("expected '{' at pos %d", p.pos)
	}
	p.pos++ // consume {
	p.skipWS()

	var fields []ObjectField
	if p.pos < len(p.src) && p.src[p.pos] == '}' {
		p.pos++
		return Pattern{Object: &ObjectPat{Fields: fields}}, nil
	}

	for {
		p.skipWS()
		// Field key — identifier or quoted string
		var key string
		var err error
		if p.pos < len(p.src) && p.src[p.pos] == '"' {
			// Quoted key
			keyPat, err := p.parseString()
			if err != nil {
				return Pattern{}, err
			}
			lit, ok := keyPat.Literal.Value.(string)
			if !ok {
				return Pattern{}, fmt.Errorf("object key must be a string at pos %d", p.pos)
			}
			key = lit
		} else {
			key = p.readIdent()
			if key == "" {
				return Pattern{}, fmt.Errorf("expected field name at pos %d", p.pos)
			}
		}
		p.skipWS()

		// Either `key: pattern` or shorthand `key` meaning `key: $key`
		var valuePat Pattern
		if p.pos < len(p.src) && p.src[p.pos] == ':' {
			p.pos++
			p.skipWS()
			valuePat, err = p.parsePattern()
			if err != nil {
				return Pattern{}, err
			}
		} else {
			// Shorthand
			valuePat = Pattern{Variable: &VariablePat{Name: key}}
		}

		fields = append(fields, ObjectField{Key: key, Pattern: valuePat})
		p.skipWS()
		if p.pos >= len(p.src) {
			return Pattern{}, fmt.Errorf("unterminated object at pos %d", p.pos)
		}
		if p.src[p.pos] == ',' {
			p.pos++
			continue
		}
		if p.src[p.pos] == '}' {
			p.pos++
			break
		}
		return Pattern{}, fmt.Errorf("expected ',' or '}' at pos %d, got %q", p.pos, p.src[p.pos])
	}
	return Pattern{Object: &ObjectPat{Fields: fields}}, nil
}

func (p *parser) parseList() (Pattern, error) {
	if p.src[p.pos] != '[' {
		return Pattern{}, fmt.Errorf("expected '[' at pos %d", p.pos)
	}
	p.pos++
	p.skipWS()

	lp := &ListPat{}
	// Empty list
	if p.pos < len(p.src) && p.src[p.pos] == ']' {
		p.pos++
		return Pattern{List: lp}, nil
	}

	for {
		p.skipWS()
		// Check for tail syntax "..."
		if p.pos+3 <= len(p.src) && p.src[p.pos:p.pos+3] == "..." {
			p.pos += 3
			p.skipWS()
			if p.pos >= len(p.src) || p.src[p.pos] != '$' {
				return Pattern{}, fmt.Errorf("expected '$var' after '...' at pos %d", p.pos)
			}
			p.pos++
			name := p.readIdent()
			if name == "" {
				return Pattern{}, fmt.Errorf("expected identifier after '...$' at pos %d", p.pos)
			}
			lp.TailVar = name
			p.skipWS()
			if p.pos >= len(p.src) || p.src[p.pos] != ']' {
				return Pattern{}, fmt.Errorf("expected ']' after tail var at pos %d", p.pos)
			}
			p.pos++
			return Pattern{List: lp}, nil
		}

		pat, err := p.parsePattern()
		if err != nil {
			return Pattern{}, err
		}
		lp.Head = append(lp.Head, pat)
		p.skipWS()
		if p.pos >= len(p.src) {
			return Pattern{}, fmt.Errorf("unterminated list at pos %d", p.pos)
		}
		if p.src[p.pos] == ',' {
			p.pos++
			continue
		}
		if p.src[p.pos] == ']' {
			p.pos++
			break
		}
		return Pattern{}, fmt.Errorf("expected ',' or ']' at pos %d, got %q", p.pos, p.src[p.pos])
	}
	return Pattern{List: lp}, nil
}

func (p *parser) parseString() (Pattern, error) {
	if p.src[p.pos] != '"' {
		return Pattern{}, fmt.Errorf("expected '\"' at pos %d", p.pos)
	}
	p.pos++
	var sb strings.Builder
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == '\\' && p.pos+1 < len(p.src) {
			next := p.src[p.pos+1]
			switch next {
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			case '"':
				sb.WriteByte('"')
			case '\\':
				sb.WriteByte('\\')
			default:
				sb.WriteByte(next)
			}
			p.pos += 2
			continue
		}
		if c == '"' {
			p.pos++
			return Pattern{Literal: &LiteralPat{Value: sb.String()}}, nil
		}
		sb.WriteByte(c)
		p.pos++
	}
	return Pattern{}, fmt.Errorf("unterminated string")
}

func (p *parser) parseNumber() (Pattern, error) {
	start := p.pos
	if p.src[p.pos] == '-' {
		p.pos++
	}
	for p.pos < len(p.src) && (unicode.IsDigit(rune(p.src[p.pos])) || p.src[p.pos] == '.') {
		p.pos++
	}
	s := p.src[start:p.pos]
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return Pattern{}, fmt.Errorf("invalid number %q at pos %d", s, start)
	}
	return Pattern{Literal: &LiteralPat{Value: n}}, nil
}

func (p *parser) parseIdentLiteral() (Pattern, error) {
	id := p.readIdent()
	switch id {
	case "true":
		return Pattern{Literal: &LiteralPat{Value: true}}, nil
	case "false":
		return Pattern{Literal: &LiteralPat{Value: false}}, nil
	case "null", "nil":
		return Pattern{Literal: &LiteralPat{Value: nil}}, nil
	}
	return Pattern{}, fmt.Errorf("unexpected identifier %q; variables must use $ prefix", id)
}

func (p *parser) readIdent() string {
	start := p.pos
	for p.pos < len(p.src) && isIdentRune(rune(p.src[p.pos])) {
		p.pos++
	}
	return p.src[start:p.pos]
}

func (p *parser) skipWS() {
	for p.pos < len(p.src) && (p.src[p.pos] == ' ' || p.src[p.pos] == '\t' || p.src[p.pos] == '\n' || p.src[p.pos] == '\r') {
		p.pos++
	}
}

func isIdentStart(c rune) bool {
	return unicode.IsLetter(c) || c == '_'
}

func isIdentRune(c rune) bool {
	return unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_'
}
