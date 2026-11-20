package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/asmseo/mdcix/internal/convert"
	"github.com/asmseo/mdcix/internal/tokens"
)

func TestInjectAccept(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	InjectAccept(req)
	if !strings.HasPrefix(req.Header.Get("Accept"), "text/markdown") {
		t.Fatalf("Accept=%q", req.Header.Get("Accept"))
	}
}

func TestInjectAcceptIfAbsentPreservesExplicit(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	req.Header.Set("Accept", "application/json")
	InjectAcceptIfAbsent(req)
	if req.Header.Get("Accept") != "application/json" {
		t.Fatalf("Accept=%q", req.Header.Get("Accept"))
	}
	req2, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	InjectAcceptIfAbsent(req2)
	if !strings.Contains(req2.Header.Get("Accept"), "text/markdown") {
		t.Fatalf("Accept=%q", req2.Header.Get("Accept"))
	}
}

func TestGetHTMLConverts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept"), "text/markdown") {
			t.Errorf("missing markdown accept: %s", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><article><h1>Title</h1><p>Paragraph with enough content for extraction thresholds to pass easily.</p></article></body></html>`))
	}))
	defer srv.Close()

	ctr, err := tokens.New("cl100k_base")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Get(context.Background(), srv.URL, Options{
		Mode:    convert.ModeReader,
		Counter: ctr,
		Client:  srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Savings.Source != "converted" {
		t.Fatalf("source=%s", res.Savings.Source)
	}
	if !strings.Contains(res.Markdown, "Title") {
		t.Fatalf("md=%s", res.Markdown)
	}
	if res.Savings.Original <= 0 {
		t.Fatal("expected original tokens")
	}
}

func TestGetNativeMarkdown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		_, _ = w.Write([]byte("# Native\n\nHello"))
	}))
	defer srv.Close()

	res, err := Get(context.Background(), srv.URL, Options{Mode: convert.ModeReader, Client: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Savings.Source != "native" {
		t.Fatalf("source=%s", res.Savings.Source)
	}
	if !strings.Contains(res.Markdown, "Native") {
		t.Fatal(res.Markdown)
	}
}

func TestApplySavingsHeaders(t *testing.T) {
	h := make(http.Header)
	ApplySavingsHeaders(h, tokens.Savings{Original: 100, Markdown: 40, Saved: 60, Percent: 60, Source: "converted"})
	if h.Get(HeaderTokensSaved) != "60" {
		t.Fatal(h)
	}
	if h.Get(HeaderSource) != "converted" {
		t.Fatal(h)
	}
}
