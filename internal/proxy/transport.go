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
// converts HTML responses, and attaches X-Mdcix-* savings headers.
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
	fetch.InjectAccept(out)
	if t.UserAgent != "" && out.Header.Get("User-Agent") == "" {
		out.Header.Set("User-Agent", t.UserAgent)
	}

	resp, err := inner.RoundTrip(out)
	if err != nil {
		return resp, err
	}

	ct := resp.Header.Get("Content-Type")
	shouldProcess := convert.IsHTML(ct) || convert.IsMarkdown(ct) ||
		strings.HasPrefix(strings.ToLower(ct), "text/") || ct == ""
	if !shouldProcess {
		return resp, nil
	}

	raw, err := readBody(resp, t.MaxBody)
	if err != nil {
		log.Printf("mdcix: read body: %v", err)
		return resp, nil
	}

	md, source, err := processBody(string(raw), ct, req.URL.String(), t.Mode)
	if err != nil {
		log.Printf("mdcix: convert: %v", err)
		resp.Body = io.NopCloser(bytes.NewReader(raw))
		resp.ContentLength = int64(len(raw))
		resp.Header.Del("Content-Encoding")
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
	resp.Header.Set("Content-Length", strconv.Itoa(len(md)))
	resp.Header.Set("Vary", "Accept")
	return resp, nil
}

func processBody(body, ct, pageURL string, mode convert.Mode) (string, string, error) {
	if convert.IsMarkdown(ct) {
		r := convert.Passthrough(body)
		return r.Markdown, r.Source, nil
	}
	if convert.IsHTML(ct) || strings.Contains(strings.ToLower(body[:min(64, len(body))]), "<html") ||
		strings.HasPrefix(strings.TrimSpace(strings.ToLower(body)), "<!doctype html") {
		res, err := convert.HTMLToMarkdown(body, convert.Options{Mode: mode, BaseURL: pageURL})
		if err != nil {
			return "", "", err
		}
		return res.Markdown, res.Source, nil
	}
	r := convert.Passthrough(body)
	return r.Markdown, "native", nil
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
