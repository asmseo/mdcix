// Package fetch retrieves upstream resources, preferring text/markdown.
package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/asmseo/mdcix/internal/convert"
	"github.com/asmseo/mdcix/internal/security"
	"github.com/asmseo/mdcix/internal/tokens"
)

// PreferMarkdown is the Accept value that asks for native markdown first.
const PreferMarkdown = "text/markdown, text/plain;q=0.9, text/html;q=0.8"

// Headers written on every mdcix markdown response.
const (
	HeaderTokensOriginal  = "X-Mdcix-Tokens-Original"
	HeaderTokensMarkdown  = "X-Mdcix-Tokens-Markdown"
	HeaderTokensSaved     = "X-Mdcix-Tokens-Saved"
	HeaderSavedPercent    = "X-Mdcix-Saved-Percent"
	HeaderSource          = "X-Mdcix-Source"
)

// Options configure an upstream fetch + conversion.
type Options struct {
	Mode      convert.Mode
	UserAgent string
	Timeout   time.Duration
	MaxBody   int64
	Insecure  bool // reserved; TLS handled by caller transport
	Counter   *tokens.Counter
	Client    *http.Client
}

// Result is markdown plus metrics.
type Result struct {
	URL      string
	Markdown string
	Savings  tokens.Savings
	Status   int
	Upstream string // original Content-Type
}

// Get fetches url, negotiates markdown, converts HTML when needed.
func Get(ctx context.Context, rawURL string, opts Options) (*Result, error) {
	if opts.Mode == "" {
		opts.Mode = convert.ModeReader
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	if opts.MaxBody <= 0 {
		opts.MaxBody = 8 << 20
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: opts.Timeout}
	}
	ctr := opts.Counter
	if ctr == nil {
		var err error
		ctr, err = tokens.Default()
		if err != nil {
			ctr = &tokens.Counter{}
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	InjectAccept(req)
	if opts.UserAgent != "" {
		req.Header.Set("User-Agent", opts.UserAgent)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := readLimited(resp.Body, opts.MaxBody)
	if err != nil {
		return nil, err
	}

	ct := resp.Header.Get("Content-Type")
	md, source, err := toMarkdown(string(body), ct, rawURL, opts.Mode)
	if err != nil {
		return nil, err
	}
	if err := security.ClassifyExtraction(resp.StatusCode, ct, string(body), md); err != nil {
		return nil, err
	}

	sv := ctr.Compute(string(body), md, source)
	return &Result{
		URL:      rawURL,
		Markdown: md,
		Savings:  sv,
		Status:   resp.StatusCode,
		Upstream: ct,
	}, nil
}

// InjectAccept sets PreferMarkdown on the request (CLI/gateway fetch paths).
func InjectAccept(req *http.Request) {
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	existing := req.Header.Get("Accept")
	if existing == "" || !strings.Contains(strings.ToLower(existing), "text/markdown") {
		req.Header.Set("Accept", PreferMarkdown)
		return
	}
	// Ensure text/markdown is first / highest priority.
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(existing)), "text/markdown") {
		req.Header.Set("Accept", PreferMarkdown)
	}
}

// InjectAcceptIfAbsent prefers markdown only when the client left Accept empty or */*.
// Used by the forward proxy so explicit client preferences (JSON, SSE, etc.) are preserved.
func InjectAcceptIfAbsent(req *http.Request) {
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	existing := strings.TrimSpace(req.Header.Get("Accept"))
	if existing == "" || existing == "*/*" {
		req.Header.Set("Accept", PreferMarkdown)
	}
}

// ApplySavingsHeaders writes X-Mdcix-* headers onto h.
func ApplySavingsHeaders(h http.Header, sv tokens.Savings) {
	h.Set(HeaderTokensOriginal, fmt.Sprintf("%d", sv.Original))
	h.Set(HeaderTokensMarkdown, fmt.Sprintf("%d", sv.Markdown))
	h.Set(HeaderTokensSaved, fmt.Sprintf("%d", sv.Saved))
	h.Set(HeaderSavedPercent, fmt.Sprintf("%.1f", sv.Percent))
	h.Set(HeaderSource, sv.Source)
}

func toMarkdown(body, ct, pageURL string, mode convert.Mode) (md, source string, err error) {
	if convert.IsMarkdown(ct) {
		r := convert.Passthrough(body)
		return r.Markdown, r.Source, nil
	}
	// Heuristic: body already looks like markdown and CT is plain/empty.
	if !convert.IsHTML(ct) && looksLikeMarkdown(body) {
		r := convert.Passthrough(body)
		return r.Markdown, r.Source, nil
	}
	if convert.IsHTML(ct) || looksLikeHTML(body) {
		res, err := convert.HTMLToMarkdown(body, convert.Options{Mode: mode, BaseURL: pageURL})
		if err != nil {
			return "", "", err
		}
		return res.Markdown, res.Source, nil
	}
	// Unknown binary-ish: return as fenced plain text stub.
	r := convert.Passthrough(body)
	return r.Markdown, "native", nil
}

func looksLikeHTML(s string) bool {
	t := strings.TrimSpace(strings.ToLower(s))
	return strings.HasPrefix(t, "<!doctype html") || strings.HasPrefix(t, "<html") || strings.Contains(t[:min(200, len(t))], "<body")
}

func looksLikeMarkdown(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return false
	}
	return strings.HasPrefix(t, "#") || strings.HasPrefix(t, "```") || strings.Contains(t, "\n## ")
}

func readLimited(r io.Reader, max int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, max))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
