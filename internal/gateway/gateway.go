// Package gateway is the shared document-processing engine for CLI, REST, MCP, and proxy.
package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/asmseo/mdcix/internal/cache"
	"github.com/asmseo/mdcix/internal/context/budget"
	"github.com/asmseo/mdcix/internal/convert"
	"github.com/asmseo/mdcix/internal/document"
	"github.com/asmseo/mdcix/internal/fetch"
	"github.com/asmseo/mdcix/internal/security"
	"github.com/asmseo/mdcix/internal/tokens"
)

// Engine orchestrates fetch → extract → document → context selection.
type Engine struct {
	Counter      *tokens.Counter
	Mode         convert.Mode
	UserAgent    string
	Timeout      time.Duration
	MaxBody      int64
	Client       *http.Client
	AllowPrivate bool // only for explicit local config
	Cache        *cache.Store
	Encoding     string
}

// LoadOptions for retrieval.
type LoadOptions struct {
	Mode convert.Mode
}

// Load retrieves and builds a structured Document.
func (e *Engine) Load(ctx context.Context, rawURL string, opts LoadOptions) (document.Document, error) {
	if !e.AllowPrivate {
		if err := security.ValidatePublicURL(rawURL); err != nil {
			return document.Document{}, err
		}
	}
	mode := opts.Mode
	if mode == "" {
		mode = e.Mode
	}
	if mode == "" {
		mode = convert.ModeReader
	}
	ctr := e.Counter
	if ctr == nil {
		var err error
		ctr, err = tokens.Default()
		if err != nil {
			ctr = &tokens.Counter{}
		}
	}
	enc := e.Encoding
	if enc == "" {
		enc = "cl100k_base"
	}
	cacheKey := cache.Key(rawURL, string(mode), enc)
	if e.Cache != nil {
		if doc, ok, err := e.Cache.Get(cacheKey); err == nil && ok {
			return doc, nil
		}
	}

	client := e.Client
	if client == nil {
		to := e.Timeout
		if to <= 0 {
			to = 30 * time.Second
		}
		client = &http.Client{Timeout: to}
	}
	maxBody := e.MaxBody
	if maxBody <= 0 {
		maxBody = 8 << 20
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return document.Document{}, err
	}
	fetch.InjectAccept(req)
	ua := e.UserAgent
	if ua == "" {
		ua = "mdcix/0.2 (+https://github.com/asmseo/mdcix)"
	}
	req.Header.Set("User-Agent", ua)

	resp, err := client.Do(req)
	if err != nil {
		return document.Document{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return document.Document{}, err
	}
	ct := resp.Header.Get("Content-Type")
	orig := string(body)
	origTokens := ctr.Count(orig)

	var md string
	method := "converted"
	if convert.IsMarkdown(ct) {
		md = strings.TrimSpace(orig)
		method = "native"
	} else if convert.IsHTML(ct) || looksHTML(orig) {
		res, err := convert.HTMLToMarkdown(orig, convert.Options{Mode: mode, BaseURL: resp.Request.URL.String()})
		if err != nil {
			return document.Document{}, fmt.Errorf("convert: %w", err)
		}
		md = res.Markdown
		method = "converted"
	} else {
		md = strings.TrimSpace(orig)
		method = "native"
	}

	if err := security.ClassifyExtraction(resp.StatusCode, ct, orig, md); err != nil {
		return document.Document{}, err
	}

	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	doc := document.BuildFromMarkdown(md, document.Document{
		URL:              rawURL,
		FinalURL:         finalURL,
		HTTPStatus:       resp.StatusCode,
		ExtractionMethod: method,
		OriginalTokens:   origTokens,
		Attribution:      finalURL,
	}, ctr)
	if e.Cache != nil {
		_ = e.Cache.Put(cacheKey, doc)
	}
	return doc, nil
}

// Outline loads (or uses) a document outline.
func (e *Engine) Outline(ctx context.Context, rawURL string) (document.Outline, error) {
	doc, err := e.Load(ctx, rawURL, LoadOptions{})
	if err != nil {
		return document.Outline{}, err
	}
	return doc.ToOutline(80), nil
}

// ReadPage returns full markdown document.
func (e *Engine) ReadPage(ctx context.Context, rawURL string) (document.Document, error) {
	return e.Load(ctx, rawURL, LoadOptions{})
}

// ReadSections loads document and packs section ids.
func (e *Engine) ReadSections(ctx context.Context, rawURL string, ids []string, tokBudget int) (budget.Response, error) {
	doc, err := e.Load(ctx, rawURL, LoadOptions{})
	if err != nil {
		return budget.Response{}, err
	}
	return budget.PackSections(doc, ids, tokBudget, e.Counter), nil
}

// ReadBudget loads document and selects by query+budget.
func (e *Engine) ReadBudget(ctx context.Context, rawURL, query string, tokBudget int) (budget.Response, error) {
	doc, err := e.Load(ctx, rawURL, LoadOptions{})
	if err != nil {
		return budget.Response{}, err
	}
	return budget.Select(doc, budget.Request{Query: query, Budget: tokBudget}, e.Counter), nil
}

func looksHTML(s string) bool {
	t := strings.TrimSpace(strings.ToLower(s))
	return strings.HasPrefix(t, "<!doctype html") || strings.HasPrefix(t, "<html")
}
