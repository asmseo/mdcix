package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

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
		// saved is clamped at 0 if markdown somehow larger
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
