package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Condition represents a parsed conditional expression
type Condition struct {
	Left     string // field or value
	Operator string // >, <, >=, <=, ==, !=, contains, not_contains
	Right    string // comparison value
}

// ConditionalBlock represents an if/else block in the DSL
// Grammar extension:
//
//	if <condition> {
//	  <statements>
//	} else {
//	  <statements>
//	}
//
// Examples:
//
//	if rain > 50 { email "you@gmail.com" }
//	if price > 100 { notify "slack" } else { save "no-alert.txt" }
//	if contains "golang" { summarize -> email "you@gmail.com" }
type ConditionalBlock struct {
	Condition Condition
	ThenBody  string // raw DSL string for then branch
	ElseBody  string // raw DSL string for else branch (optional)
}

// ParseCondition parses a condition string like "rain > 50" or "contains 'golang'"
func ParseCondition(condStr string) (Condition, error) {
	condStr = strings.TrimSpace(condStr)

	// Check for "contains" operator
	if strings.HasPrefix(condStr, "contains ") {
		value := strings.TrimPrefix(condStr, "contains ")
		value = strings.Trim(value, "\"'")
		return Condition{
			Left:     "_input",
			Operator: "contains",
			Right:    value,
		}, nil
	}

	if strings.HasPrefix(condStr, "not_contains ") {
		value := strings.TrimPrefix(condStr, "not_contains ")
		value = strings.Trim(value, "\"'")
		return Condition{
			Left:     "_input",
			Operator: "not_contains",
			Right:    value,
		}, nil
	}

	// Parse comparison operators: field OP value
	operators := []string{">=", "<=", "!=", "==", ">", "<"}
	for _, op := range operators {
		parts := strings.SplitN(condStr, op, 2)
		if len(parts) == 2 {
			return Condition{
				Left:     strings.TrimSpace(parts[0]),
				Operator: op,
				Right:    strings.Trim(strings.TrimSpace(parts[1]), "\"'"),
			}, nil
		}
	}

	// Check for truthiness: just a field name means "is non-empty"
	if condStr != "" {
		return Condition{
			Left:     condStr,
			Operator: "!=",
			Right:    "",
		}, nil
	}

	return Condition{}, fmt.Errorf("could not parse condition: %q", condStr)
}

// Evaluate evaluates a condition against the current pipeline input and extracted values
func (c *Condition) Evaluate(input string, vars map[string]string) bool {
	left := c.resolveValue(c.Left, input, vars)
	right := c.Right

	switch c.Operator {
	case "contains":
		return strings.Contains(strings.ToLower(left), strings.ToLower(right))
	case "not_contains":
		return !strings.Contains(strings.ToLower(left), strings.ToLower(right))
	case "==":
		return left == right
	case "!=":
		return left != right
	case ">", "<", ">=", "<=":
		return c.compareNumeric(left, right)
	default:
		return false
	}
}

// resolveValue resolves a field name to its value
func (c *Condition) resolveValue(field, input string, vars map[string]string) string {
	// Special field: _input refers to the pipeline input
	if field == "_input" || field == "input" {
		return input
	}

	// Check variables map (populated by previous pipeline stages)
	if val, ok := vars[field]; ok {
		return val
	}

	// Check if it's a known extracted field from weather/stock output
	lowerInput := strings.ToLower(input)
	lowerField := strings.ToLower(field)

	switch lowerField {
	case "rain", "rain%", "rain_chance", "precipitation":
		return extractNumber(input, []string{"Rain%", "rain", "precipitation", "chance of rain"})
	case "temp", "temperature":
		return extractNumber(input, []string{"Temperature:", "temperature", "°F", "°C"})
	case "price":
		return extractNumber(input, []string{"Price:", "$", "price"})
	case "change", "change%":
		return extractNumber(input, []string{"Change%", "change", "percent"})
	case "score":
		return extractNumber(input, []string{"Score:", "score", "⬆"})
	case "count", "total", "found":
		return extractNumber(input, []string{"found", "total", "count", "results"})
	}

	// If the field looks like a number, return it as-is
	if _, err := strconv.ParseFloat(field, 64); err == nil {
		return field
	}

	// Check if the input contains the field name and try to extract a value
	if strings.Contains(lowerInput, lowerField) {
		return extractNumberNear(input, field)
	}

	return field
}

// compareNumeric compares two values as numbers
func (c *Condition) compareNumeric(left, right string) bool {
	leftNum, err1 := strconv.ParseFloat(left, 64)
	rightNum, err2 := strconv.ParseFloat(right, 64)

	if err1 != nil || err2 != nil {
		// Fall back to string comparison
		switch c.Operator {
		case ">":
			return left > right
		case "<":
			return left < right
		case ">=":
			return left >= right
		case "<=":
			return left <= right
		}
		return false
	}

	switch c.Operator {
	case ">":
		return leftNum > rightNum
	case "<":
		return leftNum < rightNum
	case ">=":
		return leftNum >= rightNum
	case "<=":
		return leftNum <= rightNum
	}
	return false
}

// extractNumber finds the first number near certain keywords in the text
func extractNumber(text string, keywords []string) string {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		lowerLine := strings.ToLower(line)
		for _, kw := range keywords {
			if strings.Contains(lowerLine, strings.ToLower(kw)) {
				// Extract number from this line
				return extractFirstNumber(line)
			}
		}
	}
	return "0"
}

// extractNumberNear finds a number near a field name in text
func extractNumberNear(text, field string) string {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), strings.ToLower(field)) {
			return extractFirstNumber(line)
		}
	}
	return ""
}

// extractFirstNumber pulls the first number (int or float) from a string
func extractFirstNumber(s string) string {
	var numStr strings.Builder
	inNumber := false
	hasDecimal := false

	for _, r := range s {
		if r >= '0' && r <= '9' {
			numStr.WriteRune(r)
			inNumber = true
		} else if r == '.' && inNumber && !hasDecimal {
			numStr.WriteRune(r)
			hasDecimal = true
		} else if r == '-' && !inNumber {
			numStr.WriteRune(r)
		} else if inNumber {
			break
		}
	}

	result := numStr.String()
	if result == "" || result == "-" || result == "." {
		return "0"
	}
	return result
}

// EvaluateConditionString is a convenience function for the runtime
func EvaluateConditionString(condStr string, input string, vars map[string]string) (bool, error) {
	cond, err := ParseCondition(condStr)
	if err != nil {
		return false, err
	}

	if vars == nil {
		vars = make(map[string]string)
	}

	return cond.Evaluate(input, vars), nil
}
