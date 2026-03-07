package main

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"
)

// RetryConfig configures retry behavior
type RetryConfig struct {
	MaxAttempts  int           // max number of attempts (default 3)
	InitialDelay time.Duration // initial delay before first retry (default 1s)
	MaxDelay     time.Duration // maximum delay between retries (default 30s)
	Multiplier   float64       // delay multiplier per retry (default 2.0)
	JitterPct    float64       // random jitter as percentage of delay (default 0.25)
}

// DefaultRetryConfig returns sensible defaults
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Second,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
		JitterPct:    0.25,
	}
}

// AggressiveRetryConfig for known rate-limited APIs
func AggressiveRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:  5,
		InitialDelay: 2 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   2.5,
		JitterPct:    0.3,
	}
}

// isRetryableError checks if an error is worth retrying
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())

	retryablePatterns := []string{
		"429",
		"rate limit",
		"resource exhausted",
		"too many requests",
		"quota exceeded",
		"temporarily unavailable",
		"503",
		"502",
		"504",
		"timeout",
		"connection reset",
		"connection refused",
		"eof",
	}

	for _, pattern := range retryablePatterns {
		if strings.Contains(msg, pattern) {
			return true
		}
	}

	return false
}

// WithRetry executes a function with exponential backoff retry
func WithRetry[T any](ctx context.Context, config RetryConfig, name string, verbose bool, fn func() (T, error)) (T, error) {
	var lastErr error
	var zero T

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		result, err := fn()
		if err == nil {
			if attempt > 1 && verbose {
				fmt.Printf("[RETRY] %s succeeded on attempt %d\n", name, attempt)
			}
			return result, nil
		}

		lastErr = err

		// Don't retry non-retryable errors
		if !isRetryableError(err) {
			return zero, err
		}

		// Don't retry on last attempt
		if attempt == config.MaxAttempts {
			break
		}

		// Calculate delay with exponential backoff + jitter
		delay := config.InitialDelay * time.Duration(math.Pow(config.Multiplier, float64(attempt-1)))
		if delay > config.MaxDelay {
			delay = config.MaxDelay
		}

		// Add jitter
		jitter := time.Duration(float64(delay) * config.JitterPct * (rand.Float64()*2 - 1))
		delay += jitter
		if delay < 0 {
			delay = config.InitialDelay
		}

		if verbose {
			fmt.Printf("[RETRY] %s attempt %d/%d failed: %v. Retrying in %v...\n",
				name, attempt, config.MaxAttempts, err, delay.Round(time.Millisecond))
		}

		// Wait with context awareness
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(delay):
		}
	}

	return zero, fmt.Errorf("%s failed after %d attempts: %w", name, config.MaxAttempts, lastErr)
}

// WithRetryString is a convenience wrapper for string-returning functions
func WithRetryString(ctx context.Context, config RetryConfig, name string, verbose bool, fn func() (string, error)) (string, error) {
	return WithRetry(ctx, config, name, verbose, fn)
}

// RetryableCommand wraps a runtime command execution with retry
// Usage in runtime.go:
//
//	case "search":
//	    result, err = RetryableCommand(ctx, r.verbose, "SEARCH", func() (string, error) {
//	        return r.search(ctx, cmd.Arg)
//	    })
func RetryableCommand(ctx context.Context, verbose bool, cmdName string, fn func() (string, error)) (string, error) {
	config := DefaultRetryConfig()

	// Use aggressive retry for known rate-limited commands
	switch strings.ToLower(cmdName) {
	case "search", "ask", "summarize", "analyze", "translate", "image_generate", "video_generate", "tts":
		config = AggressiveRetryConfig() // Gemini is often rate-limited on free tier
	case "crypto":
		config.MaxAttempts = 3
		config.InitialDelay = 5 * time.Second // CoinGecko rate limits are per-minute
	}

	return WithRetryString(ctx, config, cmdName, verbose, fn)
}
