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
	"github.com/asmseo/mdcix/internal/policy"
	"github.com/asmseo/mdcix/internal/security"
	"github.com/asmseo/mdcix/internal/telemetry"
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
	Policy       *policy.Checker
	Telemetry    telemetry.Sink
}

// LoadOptions for retrieval.
type LoadOptions struct {
	Mode            convert.Mode
	DocumentVersion string // when set, warn if snapshot mismatches
}

// Load retrieves and builds a structured Document.
func (e *Engine) Load(ctx context.Context, rawURL string, opts LoadOptions) (document.Document, error) {
	start := time.Now()
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

	var policyDec policy.Decision
	if e.Policy != nil {
		policyDec = e.Policy.Evaluate(ctx, rawURL)
		if !policyDec.Allow {
			return document.Document{}, fmt.Errorf("policy denied: %v", policyDec.Warnings)
		}
	}

	if e.Cache != nil {
		if doc, meta, ok, err := e.Cache.GetWithMeta(cacheKey); err == nil && ok {
			// Within TTL: serve cached. If validators exist, probe If-None-Match;
			// on 304 keep cache; on 200 fall through to replace; on network error keep stale.
			if meta.ETag != "" || meta.LastModified != "" {
				status, err := e.conditionalStatus(ctx, rawURL, meta)
				if err == nil && status == http.StatusOK {
					// upstream changed — fall through to full fetch
				} else {
					e.record("cache_hit", start, map[string]int{"hit": 1}, map[string]string{"revalidate": fmt.Sprintf("%d", status)})
					return withVersionWarn(doc, opts.DocumentVersion), nil
				}
			} else {
				e.record("cache_hit", start, map[string]int{"hit": 1}, nil)
				return withVersionWarn(doc, opts.DocumentVersion), nil
			}
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
		e.record("extraction_failure", start, nil, map[string]string{"reason": err.Error()})
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
	if e.Policy != nil {
		doc.Warnings = append(doc.Warnings, policyDec.Warnings...)
		doc.Warnings = append(doc.Warnings, policy.ContentSignalsNote())
		if doc.Attribution == "" {
			doc.Attribution = policyDec.Attribution
		}
	}
	doc = withVersionWarn(doc, opts.DocumentVersion)
	if e.Cache != nil {
		_ = e.Cache.PutWithMeta(cacheKey, doc, metaFromResp(resp))
	}
	e.record("load", start, map[string]int{
		"original_tokens": doc.OriginalTokens,
		"markdown_tokens": doc.MarkdownTokens,
		"sections":        len(doc.Sections),
	}, map[string]string{"method": method})
	return doc, nil
}

func withVersionWarn(doc document.Document, want string) document.Document {
	if want != "" && doc.Version != want {
		doc.Warnings = append(doc.Warnings,
			fmt.Sprintf("document_version_mismatch: requested=%s have=%s", want, doc.Version))
	}
	return doc
}

func (e *Engine) conditionalStatus(ctx context.Context, rawURL string, meta cache.EntryMeta) (int, error) {
	client := e.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, err
	}
	if meta.ETag != "" {
		req.Header.Set("If-None-Match", meta.ETag)
	}
	if meta.LastModified != "" {
		req.Header.Set("If-Modified-Since", meta.LastModified)
	}
	fetch.InjectAccept(req)
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, nil
}

func metaFromResp(resp *http.Response) cache.EntryMeta {
	if resp == nil {
		return cache.EntryMeta{}
	}
	return cache.EntryMeta{
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		CacheControl: resp.Header.Get("Cache-Control"),
		Vary:         resp.Header.Get("Vary"),
	}
}

func (e *Engine) record(name string, start time.Time, ints map[string]int, labels map[string]string) {
	sink := e.Telemetry
	if sink == nil {
		sink = telemetry.Nop{}
	}
	sink.Record(telemetry.Event{Name: name, Duration: time.Since(start), Ints: ints, Labels: labels})
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
	return e.ReadSectionsVersioned(ctx, rawURL, ids, tokBudget, "")
}

// ReadSectionsVersioned packs sections and warns on document_version mismatch.
func (e *Engine) ReadSectionsVersioned(ctx context.Context, rawURL string, ids []string, tokBudget int, version string) (budget.Response, error) {
	doc, err := e.Load(ctx, rawURL, LoadOptions{DocumentVersion: version})
	if err != nil {
		return budget.Response{}, err
	}
	res := budget.PackSections(doc, ids, tokBudget, e.Counter)
	if version != "" && doc.Version != version {
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("document_version_mismatch: requested=%s have=%s", version, doc.Version))
	}
	return res, nil
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
