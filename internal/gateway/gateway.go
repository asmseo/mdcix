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
	"github.com/asmseo/mdcix/internal/render"
	"github.com/asmseo/mdcix/internal/security"
	"github.com/asmseo/mdcix/internal/telemetry"
	"github.com/asmseo/mdcix/internal/tokens"
)

// Engine orchestrates fetch → extract → document → context selection.
type Engine struct {
	Counter      *tokens.Counter
	Mode         convert.Mode
	Plain        bool // strip navigational links (opt-in)
	RenderJS     render.Mode
	RenderWait   time.Duration
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
	Plain           *bool
	RenderJS        render.Mode // empty = engine default
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
	plain := e.Plain
	if opts.Plain != nil {
		plain = *opts.Plain
	}
	jsMode := e.RenderJS
	if opts.RenderJS != "" {
		jsMode = opts.RenderJS
	}
	if jsMode == "" {
		jsMode = render.ModeOff
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
	cacheKey := cache.KeyFull(rawURL, string(mode), enc, plain, string(jsMode))

	var policyDec policy.Decision
	if e.Policy != nil {
		policyDec = e.Policy.Evaluate(ctx, rawURL)
		if !policyDec.Allow {
			return document.Document{}, fmt.Errorf("policy denied: %v", policyDec.Warnings)
		}
	}

	if e.Cache != nil {
		if doc, meta, ok, err := e.Cache.GetWithMeta(cacheKey); err == nil && ok {
			if meta.ETag != "" || meta.LastModified != "" {
				status, err := e.conditionalStatus(ctx, rawURL, meta)
				if err == nil && status == http.StatusOK {
					// upstream changed — fall through
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

	ua := e.UserAgent
	if ua == "" {
		ua = "mdcix/0.2 (+https://github.com/asmseo/mdcix)"
	}

	var (
		orig       string
		ct         string
		status     int
		finalURL   = rawURL
		etag       string
		lastMod    string
		cacheCtl   string
		vary       string
		usedRender bool
		warnings   []string
	)

	forceJS := jsMode == render.ModeOn
	if !forceJS {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return document.Document{}, err
		}
		fetch.InjectAccept(req)
		req.Header.Set("User-Agent", ua)

		resp, err := client.Do(req)
		if err != nil {
			return document.Document{}, err
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
		_ = resp.Body.Close()
		if err != nil {
			return document.Document{}, err
		}
		orig = string(body)
		ct = resp.Header.Get("Content-Type")
		status = resp.StatusCode
		etag = resp.Header.Get("ETag")
		lastMod = resp.Header.Get("Last-Modified")
		cacheCtl = resp.Header.Get("Cache-Control")
		vary = resp.Header.Get("Vary")
		if resp.Request != nil && resp.Request.URL != nil {
			finalURL = resp.Request.URL.String()
		}
	}

	convertOpts := convert.Options{Mode: mode, BaseURL: finalURL, Plain: plain}
	var md string
	method := "converted"

	buildMD := func(htmlOrBody, contentType string) (string, string, error) {
		if convert.IsMarkdown(contentType) {
			out := strings.TrimSpace(htmlOrBody)
			if plain {
				out = convert.StripLinks(out)
			}
			return out, "native", nil
		}
		if convert.IsHTML(contentType) || looksHTML(htmlOrBody) {
			res, err := convert.HTMLToMarkdown(htmlOrBody, convertOpts)
			if err != nil {
				return "", "", err
			}
			return res.Markdown, "converted", nil
		}
		out := strings.TrimSpace(htmlOrBody)
		if plain {
			out = convert.StripLinks(out)
		}
		return out, "native", nil
	}

	if forceJS {
		html, err := e.renderHTML(ctx, rawURL, ua)
		if err != nil {
			e.record("render_failure", start, nil, map[string]string{"reason": err.Error()})
			return document.Document{}, err
		}
		orig = html
		ct = "text/html; charset=utf-8"
		status = 200
		usedRender = true
		warnings = append(warnings, "js_rendered:headless_chrome")
	}

	var err error
	md, method, err = buildMD(orig, ct)
	if err != nil {
		return document.Document{}, fmt.Errorf("convert: %w", err)
	}

	needJS := jsMode == render.ModeAuto && !usedRender &&
		(security.ClassifyExtraction(status, ct, orig, md) != nil || render.NeedsAuto(orig, md))

	if needJS {
		html, rerr := e.renderHTML(ctx, rawURL, ua)
		if rerr != nil {
			warnings = append(warnings, "js_render_failed:"+rerr.Error())
			// keep static result; if classify fails, surface that error
			if cerr := security.ClassifyExtraction(status, ct, orig, md); cerr != nil {
				e.record("extraction_failure", start, nil, map[string]string{"reason": cerr.Error()})
				return document.Document{}, cerr
			}
		} else {
			orig = html
			ct = "text/html; charset=utf-8"
			status = 200
			usedRender = true
			warnings = append(warnings, "js_rendered:auto_headless_chrome")
			md, method, err = buildMD(orig, ct)
			if err != nil {
				return document.Document{}, fmt.Errorf("convert: %w", err)
			}
		}
	}

	if err := security.ClassifyExtraction(status, ct, orig, md); err != nil {
		e.record("extraction_failure", start, nil, map[string]string{"reason": err.Error()})
		return document.Document{}, err
	}

	origTokens := ctr.Count(orig)
	doc := document.BuildFromMarkdown(md, document.Document{
		URL:              rawURL,
		FinalURL:         finalURL,
		HTTPStatus:       status,
		ExtractionMethod: method,
		OriginalTokens:   origTokens,
		Attribution:      finalURL,
		Warnings:         warnings,
	}, ctr)
	if usedRender {
		doc.ExtractionMethod = method + "+js"
	}
	if e.Policy != nil {
		doc.Warnings = append(doc.Warnings, policyDec.Warnings...)
		doc.Warnings = append(doc.Warnings, policy.ContentSignalsNote())
		if doc.Attribution == "" {
			doc.Attribution = policyDec.Attribution
		}
	}
	doc = withVersionWarn(doc, opts.DocumentVersion)
	if e.Cache != nil {
		_ = e.Cache.PutWithMeta(cacheKey, doc, cache.EntryMeta{
			ETag:         etag,
			LastModified: lastMod,
			CacheControl: cacheCtl,
			Vary:         vary,
		})
	}
	e.record("load", start, map[string]int{
		"original_tokens": doc.OriginalTokens,
		"markdown_tokens": doc.MarkdownTokens,
		"sections":        len(doc.Sections),
	}, map[string]string{"method": doc.ExtractionMethod, "js": string(jsMode)})
	return doc, nil
}

func (e *Engine) renderHTML(ctx context.Context, rawURL, ua string) (string, error) {
	wait := e.RenderWait
	if wait <= 0 {
		wait = 15 * time.Second
	}
	return render.HTML(ctx, rawURL, render.Options{
		Timeout:   wait,
		UserAgent: ua,
		WaitMS:    8000,
	})
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
