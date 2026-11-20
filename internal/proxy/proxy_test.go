package proxy

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/asmseo/mdcix/internal/convert"
	"github.com/asmseo/mdcix/internal/fetch"
	"github.com/asmseo/mdcix/internal/tokens"
)

func TestMarkdownTransportConvertsAndHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept"), "text/markdown") {
			t.Errorf("Accept=%q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><main><h1>Proxy</h1><p>Content long enough for the extractor to keep the main region as primary.</p></main></body></html>`))
	}))
	defer upstream.Close()

	ctr, err := tokens.New("cl100k_base")
	if err != nil {
		t.Fatal(err)
	}
	tr := &MarkdownTransport{
		Inner:   upstream.Client().Transport,
		Mode:    convert.ModeReader,
		MaxBody: 1 << 20,
		Counter: ctr,
	}

	req, _ := http.NewRequest(http.MethodGet, upstream.URL, nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if !strings.Contains(string(body), "Proxy") {
		t.Fatalf("body=%s", body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "markdown") {
		t.Fatalf("ct=%s", ct)
	}
	orig, _ := strconv.Atoi(resp.Header.Get(fetch.HeaderTokensOriginal))
	md, _ := strconv.Atoi(resp.Header.Get(fetch.HeaderTokensMarkdown))
	saved, _ := strconv.Atoi(resp.Header.Get(fetch.HeaderTokensSaved))
	if orig <= 0 || md <= 0 {
		t.Fatalf("tokens original=%d markdown=%d", orig, md)
	}
	if saved != orig-md && saved != 0 {
		if orig >= md && saved != orig-md {
			t.Fatalf("saved=%d want %d", saved, orig-md)
		}
	}
	if resp.Header.Get(fetch.HeaderSource) != "converted" {
		t.Fatal(resp.Header.Get(fetch.HeaderSource))
	}
	if resp.Header.Get(fetch.HeaderSavedPercent) == "" {
		t.Fatal("missing percent header")
	}
}

func TestPassThroughJSON(t *testing.T) {
	payload := `{"ok":true,"items":[1,2,3]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	defer upstream.Close()

	tr := &MarkdownTransport{Inner: upstream.Client().Transport, Mode: convert.ModeReader, MaxBody: 1 << 20}
	req, _ := http.NewRequest(http.MethodGet, upstream.URL, nil)
	req.Header.Set("Accept", "application/json")
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != payload {
		t.Fatalf("json altered: %s", body)
	}
	if resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("ct=%s", resp.Header.Get("Content-Type"))
	}
	if resp.Header.Get(fetch.HeaderTokensSaved) != "" {
		t.Fatal("must not attach savings headers on JSON pass-through")
	}
}

func TestPassThroughSSE(t *testing.T) {
	events := "data: hello\n\ndata: world\n\n"
	var sawAccept string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte(events))
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	tr := &MarkdownTransport{Inner: upstream.Client().Transport, Mode: convert.ModeReader, MaxBody: 1 << 20}
	req, _ := http.NewRequest(http.MethodGet, upstream.URL, nil)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != events {
		t.Fatalf("SSE altered: %q", body)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "event-stream") {
		t.Fatalf("ct=%s", resp.Header.Get("Content-Type"))
	}
	if sawAccept != "text/event-stream" {
		t.Fatalf("client Accept not preserved: %q", sawAccept)
	}
	if resp.Header.Get(fetch.HeaderSource) != "" {
		t.Fatal("SSE must not be converted")
	}
}

func TestPreserveExplicitJSONAccept(t *testing.T) {
	var saw string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		saw = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()
	tr := &MarkdownTransport{Inner: upstream.Client().Transport, Mode: convert.ModeReader}
	req, _ := http.NewRequest(http.MethodGet, upstream.URL, nil)
	req.Header.Set("Accept", "application/json")
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if saw != "application/json" {
		t.Fatalf("Accept overwritten to %q", saw)
	}
}

func TestPassThroughEmptyContentType(t *testing.T) {
	payload := "raw-bytes-not-html"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// no Content-Type
		_, _ = w.Write([]byte(payload))
	}))
	defer upstream.Close()
	tr := &MarkdownTransport{Inner: upstream.Client().Transport, Mode: convert.ModeReader, MaxBody: 1 << 20}
	req, _ := http.NewRequest(http.MethodGet, upstream.URL, nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != payload {
		t.Fatalf("altered unknown body: %s", body)
	}
}

func TestEligibleHelpers(t *testing.T) {
	html := &http.Response{Header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}}, ContentLength: 100}
	if !eligibleForConversion(html) {
		t.Fatal("html should be eligible")
	}
	sse := &http.Response{Header: http.Header{"Content-Type": []string{"text/event-stream"}}, ContentLength: -1}
	if eligibleForConversion(sse) {
		t.Fatal("sse must not be eligible")
	}
	js := &http.Response{Header: http.Header{"Content-Type": []string{"application/javascript"}}, ContentLength: 10}
	if eligibleForConversion(js) {
		t.Fatal("js must not be eligible")
	}
}

func TestProxyServerRoundTripHTML(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><main><h1>Srv</h1><p>Enough text for the reader extractor to retain the main article region.</p></main></body></html>`))
	}))
	defer origin.Close()

	ctr, _ := tokens.Default()
	proxySrv := New(Options{Addr: "127.0.0.1:0", Mode: convert.ModeReader, Counter: ctr, Timeout: 5 * time.Second})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	proxySrv.httpServer.Addr = ln.Addr().String()
	defer proxySrv.Close()
	go func() { _ = proxySrv.httpServer.Serve(ln) }()

	proxyURL, _ := url.Parse("http://" + ln.Addr().String())
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	resp, err := client.Get(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Srv") {
		t.Fatalf("body=%s", body)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "markdown") {
		t.Fatalf("ct=%s", resp.Header.Get("Content-Type"))
	}
}

func TestInjectAcceptIfAbsentUsedByProxy(t *testing.T) {
	var saw string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		saw = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><main><h1>A</h1><p>Body text long enough for extraction.</p></main></body></html>`))
	}))
	defer upstream.Close()
	tr := &MarkdownTransport{Inner: upstream.Client().Transport, Mode: convert.ModeReader, MaxBody: 1 << 20}
	req, _ := http.NewRequest(http.MethodGet, upstream.URL, nil)
	// no Accept → proxy injects markdown preference
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !strings.Contains(saw, "text/markdown") {
		t.Fatalf("expected markdown accept injection, got %q", saw)
	}
}
