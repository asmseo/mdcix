package proxy

import (
	"bytes"
	"compress/gzip"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/asmseo/mdcix/internal/convert"
	"github.com/asmseo/mdcix/internal/fetch"
	"github.com/asmseo/mdcix/internal/stats"
	"github.com/asmseo/mdcix/internal/tokens"
)

// MarkdownTransport is an http.RoundTripper that prefers markdown upstream,
// converts eligible HTML responses, and attaches X-Mdcix-* savings headers.
//
// Non-eligible responses (JSON, binary, SSE/streaming, JS/CSS, etc.) pass through
// unchanged — the body is not buffered.
type MarkdownTransport struct {
	Inner     http.RoundTripper
	Mode      convert.Mode
	MaxBody   int64
	Counter   *tokens.Counter
	Session   *stats.Session
	UserAgent string
}

// RoundTrip implements http.RoundTripper.
func (t *MarkdownTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	inner := t.Inner
	if inner == nil {
		inner = http.DefaultTransport
	}

	// Clone so we can mutate Accept / UA without affecting the caller.
	out := req.Clone(req.Context())
	fetch.InjectAcceptIfAbsent(out)
	if t.UserAgent != "" && out.Header.Get("User-Agent") == "" {
		out.Header.Set("User-Agent", t.UserAgent)
	}

	resp, err := inner.RoundTrip(out)
	if err != nil {
		return resp, err
	}

	if !eligibleForConversion(resp) {
		return resp, nil
	}

	raw, err := readBody(resp, t.MaxBody)
	if err != nil {
		log.Printf("mdcix: read body: %v", err)
		return resp, nil
	}

	md, source, err := processBody(string(raw), resp.Header.Get("Content-Type"), req.URL.String(), t.Mode)
	if err != nil {
		log.Printf("mdcix: convert: %v", err)
		resp.Body = io.NopCloser(bytes.NewReader(raw))
		resp.ContentLength = int64(len(raw))
		resp.Header.Del("Content-Encoding")
		resp.Header.Del("Transfer-Encoding")
		resp.Header.Set("Content-Length", strconv.Itoa(len(raw)))
		return resp, nil
	}

	ctr := t.Counter
	if ctr == nil {
		ctr, _ = tokens.Default()
		if ctr == nil {
			ctr = &tokens.Counter{}
		}
	}
	sv := ctr.Compute(string(raw), md, source)
	if t.Session != nil {
		t.Session.Add(sv)
	}
	log.Printf("mdcix tokens url=%s source=%s original=%d markdown=%d saved=%d (%.1f%%)",
		req.URL.String(), sv.Source, sv.Original, sv.Markdown, sv.Saved, sv.Percent)

	fetch.ApplySavingsHeaders(resp.Header, sv)
	resp.Body = io.NopCloser(strings.NewReader(md))
	resp.ContentLength = int64(len(md))
	resp.Header.Set("Content-Type", "text/markdown; charset=utf-8")
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("Transfer-Encoding")
	resp.Header.Set("Content-Length", strconv.Itoa(len(md)))
	resp.Header.Set("Vary", "Accept")
	return resp, nil
}

// eligibleForConversion reports whether the response may be buffered and converted.
// Streaming, SSE, JSON, binary, and other non-document types must pass through.
func eligibleForConversion(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if isPassThroughContentType(ct) {
		return false
	}
	if isStreamingResponse(resp) {
		return false
	}
	// Only convert clearly HTML or Markdown documents.
	return convert.IsHTML(ct) || convert.IsMarkdown(ct)
}

func isPassThroughContentType(ct string) bool {
	if ct == "" {
		return true // unknown type — do not intercept
	}
	switch {
	case strings.Contains(ct, "text/event-stream"),
		strings.Contains(ct, "application/stream"),
		strings.Contains(ct, "application/x-ndjson"),
		strings.Contains(ct, "application/json"),
		strings.Contains(ct, "application/javascript"),
		strings.Contains(ct, "text/javascript"),
		strings.Contains(ct, "text/css"),
		strings.Contains(ct, "text/csv"),
		strings.Contains(ct, "multipart/"),
		strings.Contains(ct, "image/"),
		strings.Contains(ct, "audio/"),
		strings.Contains(ct, "video/"),
		strings.Contains(ct, "font/"),
		strings.Contains(ct, "application/octet-stream"),
		strings.Contains(ct, "application/pdf"),
		strings.Contains(ct, "application/zip"),
		strings.Contains(ct, "application/wasm"),
		strings.Contains(ct, "application/grpc"):
		return true
	default:
		return false
	}
}

func isStreamingResponse(resp *http.Response) bool {
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "event-stream") || strings.Contains(ct, "stream") {
		return true
	}
	// Chunked responses without a definite length are treated as potentially streaming
	// unless they are clearly HTML/Markdown documents we intend to convert.
	if resp.ContentLength < 0 {
		te := strings.ToLower(resp.Header.Get("Transfer-Encoding"))
		if strings.Contains(te, "chunked") && !convert.IsHTML(ct) && !convert.IsMarkdown(ct) {
			return true
		}
	}
	return false
}

func processBody(body, ct, pageURL string, mode convert.Mode) (string, string, error) {
	if convert.IsMarkdown(ct) {
		r := convert.Passthrough(body)
		return r.Markdown, r.Source, nil
	}
	if convert.IsHTML(ct) || looksLikeHTMLPrefix(body) {
		res, err := convert.HTMLToMarkdown(body, convert.Options{Mode: mode, BaseURL: pageURL})
		if err != nil {
			return "", "", err
		}
		return res.Markdown, res.Source, nil
	}
	r := convert.Passthrough(body)
	return r.Markdown, "native", nil
}

func looksLikeHTMLPrefix(body string) bool {
	n := min(64, len(body))
	if n == 0 {
		return false
	}
	prefix := strings.ToLower(body[:n])
	trim := strings.TrimSpace(strings.ToLower(body))
	return strings.Contains(prefix, "<html") || strings.HasPrefix(trim, "<!doctype html")
}

func readBody(resp *http.Response, max int64) ([]byte, error) {
	defer resp.Body.Close()
	var r io.Reader = resp.Body
	if max > 0 {
		r = io.LimitReader(resp.Body, max)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	enc := strings.ToLower(resp.Header.Get("Content-Encoding"))
	if strings.Contains(enc, "gzip") {
		gr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return data, nil // return raw
		}
		defer gr.Close()
		return io.ReadAll(gr)
	}
	return data, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
