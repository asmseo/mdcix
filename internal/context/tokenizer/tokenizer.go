// Package tokenizer adapts the shared tiktoken counter for the context engine.
//
// Estimated tokens (cl100k_base) are distinct from model-provider billing.
package tokenizer

import "github.com/asmseo/mdcix/internal/tokens"

// Counter estimates tokens for budget packing.
type Counter interface {
	Count(text string) int
}

// Wrap returns a Counter from a tokens.Counter (nil-safe).
func Wrap(c *tokens.Counter) Counter {
	if c == nil {
		c, _ = tokens.Default()
		if c == nil {
			return &tokens.Counter{}
		}
	}
	return c
}

// EncodingName is the default estimation encoding.
const EncodingName = "cl100k_base"

// Note clarifies estimate vs billing for agent-facing surfaces.
const Note = "Token counts are estimates using " + EncodingName + ", not provider billing."
