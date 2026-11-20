// Package render fetches pages with optional headless Chrome (JS rendering).
//
// Opt-in only. Uses the system Chrome/Chromium CLI (--dump-dom) with a hard
// process timeout so SPA pages cannot hang the agent forever.
package render

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Mode controls when headless rendering runs.
type Mode string

const (
	ModeOff  Mode = "off"  // never
	ModeOn   Mode = "on"   // always
	ModeAuto Mode = "auto" // on empty/thin extraction after static fetch
)

// Options for a headless render.
type Options struct {
	Timeout   time.Duration // hard kill; default 20s
	UserAgent string
	ChromePath string // empty = auto-detect
	WaitMS    int     // virtual-time-budget; default 4000
}

// ErrNoChrome means no Chrome/Chromium binary was found.
var ErrNoChrome = errors.New("render: chrome/chromium not found (install Google Chrome or Chromium)")

// ParseMode accepts off|on|auto|true|false|1|0.
func ParseMode(s string) Mode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "true", "1", "yes", "always":
		return ModeOn
	case "auto":
		return ModeAuto
	default:
		return ModeOff
	}
}

// FindChrome returns the first available browser binary.
func FindChrome() (string, error) {
	candidates := []string{
		"google-chrome",
		"google-chrome-stable",
		"chromium",
		"chromium-browser",
		"chrome",
		"/usr/bin/google-chrome",
		"/usr/bin/google-chrome-stable",
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	}
	for _, c := range candidates {
		if !strings.Contains(c, "/") {
			if path, err := exec.LookPath(c); err == nil {
				return path, nil
			}
			continue
		}
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	return "", ErrNoChrome
}

// HTML loads url in headless Chrome and returns the post-JS DOM as HTML.
func HTML(ctx context.Context, rawURL string, opts Options) (string, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 20 * time.Second
	}
	if opts.WaitMS <= 0 {
		opts.WaitMS = 4000
	}
	chrome := opts.ChromePath
	if chrome == "" {
		var err error
		chrome, err = FindChrome()
		if err != nil {
			return "", err
		}
	}

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	args := []string{
		"--headless=new",
		"--disable-gpu",
		"--no-sandbox",
		"--disable-dev-shm-usage",
		"--hide-scrollbars",
		"--dump-dom",
		fmt.Sprintf("--virtual-time-budget=%d", opts.WaitMS),
	}
	if opts.UserAgent != "" {
		args = append(args, "--user-agent="+opts.UserAgent)
	}
	args = append(args, rawURL)

	cmd := exec.CommandContext(ctx, chrome, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	html := stdout.String()
	// Many SPAs never idle; Chrome may hang after painting a useful DOM.
	// If we already have substantial HTML, accept it even on timeout/kill.
	if usableDOM(html) {
		return html, nil
	}
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("render: chrome timed out after %s for %s (no usable DOM)", opts.Timeout, rawURL)
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("render: chrome failed: %s", truncate(msg, 400))
	}
	if strings.TrimSpace(html) == "" {
		return "", fmt.Errorf("render: empty DOM for %s", rawURL)
	}
	return html, nil
}

func usableDOM(html string) bool {
	s := strings.TrimSpace(html)
	if len(s) < 1500 {
		return false
	}
	low := strings.ToLower(s)
	return strings.Contains(low, "<html") || strings.Contains(low, "<body") || strings.Contains(low, "<div")
}

// NeedsAuto reports whether static extraction looks thin enough to retry with JS.
func NeedsAuto(originalHTML, markdown string) bool {
	md := strings.TrimSpace(markdown)
	if md == "" {
		return true
	}
	// Very short markdown vs large HTML shell (SPA).
	if len(md) < 200 && len(originalHTML) > 8_000 {
		return true
	}
	// Markdown tiny relative to visible text estimate after stripping scripts.
	if len(md) < 120 {
		return true
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
