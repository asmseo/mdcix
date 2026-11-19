// Package tokens estimates LLM token counts (cl100k_base / tiktoken-compatible).
package tokens

import (
	"fmt"
	"sync"

	"github.com/pkoukk/tiktoken-go"
)

// Counter counts tokens with a TikToken encoding.
type Counter struct {
	enc *tiktoken.Tiktoken
}

var (
	defaultOnce sync.Once
	defaultCtr  *Counter
	defaultErr  error
)

// New creates a Counter for encoding (e.g. "cl100k_base", "o200k_base").
func New(encoding string) (*Counter, error) {
	if encoding == "" {
		encoding = "cl100k_base"
	}
	enc, err := tiktoken.GetEncoding(encoding)
	if err != nil {
		return nil, fmt.Errorf("tiktoken encoding %q: %w", encoding, err)
	}
	return &Counter{enc: enc}, nil
}

// Default returns a process-wide cl100k_base counter.
func Default() (*Counter, error) {
	defaultOnce.Do(func() {
		defaultCtr, defaultErr = New("cl100k_base")
	})
	return defaultCtr, defaultErr
}

// Count returns the number of tokens in text.
func (c *Counter) Count(text string) int {
	if c == nil || c.enc == nil {
		return Approx(text)
	}
	return len(c.enc.Encode(text, nil, nil))
}

// Approx is a fast heuristic (~4 chars/token) used when tiktoken is unavailable.
func Approx(text string) int {
	n := len(text)
	if n == 0 {
		return 0
	}
	return (n + 3) / 4
}

// Savings holds original vs markdown token metrics.
type Savings struct {
	Original int
	Markdown int
	Saved    int
	Percent  float64
	Source   string // "native" | "converted"
}

// Compute builds Savings from original and markdown text.
func (c *Counter) Compute(original, markdown, source string) Savings {
	orig := c.Count(original)
	md := c.Count(markdown)
	saved := orig - md
	if saved < 0 {
		saved = 0
	}
	var pct float64
	if orig > 0 {
		pct = float64(saved) / float64(orig) * 100
	}
	return Savings{
		Original: orig,
		Markdown: md,
		Saved:    saved,
		Percent:  pct,
		Source:   source,
	}
}
