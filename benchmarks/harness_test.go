package benchmarks_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/asmseo/mdcix/internal/context/budget"
	"github.com/asmseo/mdcix/internal/convert"
	"github.com/asmseo/mdcix/internal/gateway"
	"github.com/asmseo/mdcix/internal/tokens"
)

type caseSpec struct {
	File     string
	Query    string
	Budget   int
	Sections []string // optional preferred section title substrings for agent-selected path
}

func fixtureDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Join(filepath.Dir(file), "fixtures")
}

func TestLocalBudgetReductionHarness(t *testing.T) {
	dir := fixtureDir(t)
	cases := []caseSpec{
		{File: "en-docs-structured.html", Query: "pricing plans and integrations", Budget: 400},
		{File: "pt-saas-produto.html", Query: "planos e preços WhatsApp", Budget: 350},
		{File: "es-noticia.html", Query: "limitaciones BM25 idiomas", Budget: 300},
		{File: "unstructured-smallbiz.html", Query: "hours parking contact", Budget: 200},
	}

	ctr, err := tokens.Default()
	if err != nil {
		t.Fatal(err)
	}

	var rows []string
	rows = append(rows, "fixture\thtml_tokens\tmd_tokens\tbudget_tokens\toutline_tokens\treduction_vs_md\tfailure")

	for _, c := range cases {
		htmlBytes, err := os.ReadFile(filepath.Join(dir, c.File))
		if err != nil {
			t.Fatal(err)
		}
		html := string(htmlBytes)
		htmlTok := ctr.Count(html)

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(htmlBytes)
		}))

		eng := &gateway.Engine{
			Counter:      ctr,
			Mode:         convert.ModeReader,
			Client:       ts.Client(),
			AllowPrivate: true,
			Timeout:      5 * time.Second,
			Encoding:     "cl100k_base",
		}

		doc, err := eng.ReadPage(context.Background(), ts.URL)
		ts.Close()
		if err != nil {
			rows = append(rows, strings.Join([]string{c.File, itoa(htmlTok), "-", "-", "-", "n/a", err.Error()}, "\t"))
			t.Errorf("%s: unexpected load error: %v", c.File, err)
			continue
		}
		mdTok := doc.MarkdownTokens
		if mdTok <= 0 {
			mdTok = ctr.Count(doc.Markdown)
		}

		outlineApprox := 0
		for _, s := range doc.ToOutline(80).Sections {
			outlineApprox += 8 + ctr.Count(s.Title) + ctr.Count(s.Preview)/4
		}

		res := budget.Select(doc, budget.Request{Query: c.Query, Budget: c.Budget}, ctr)
		ret := res.ReturnedTokens
		if ret < 0 {
			ret = 0
		}
		reduction := 0.0
		if mdTok > 0 {
			reduction = 100 * float64(mdTok-ret) / float64(mdTok)
		}
		// Never treat empty/failed selection as success if markdown was empty.
		failure := ""
		if doc.Markdown == "" {
			failure = "empty_markdown"
			reduction = 0
		}
		if ret == 0 && mdTok > 0 {
			failure = "no_sections_selected"
			// still report reduction honestly; harness must not hide it
		}
		rows = append(rows, strings.Join([]string{
			c.File,
			itoa(htmlTok),
			itoa(mdTok),
			itoa(ret),
			itoa(outlineApprox),
			sprintf1(reduction),
			failure,
		}, "\t"))

		if mdTok >= htmlTok && htmlTok > 50 {
			t.Logf("%s: markdown tokens (%d) not lower than html (%d) — possible, logged", c.File, mdTok, htmlTok)
		}
		if failure == "" && ret > c.Budget+50 {
			t.Errorf("%s: returned_tokens %d exceeds budget %d", c.File, ret, c.Budget)
		}
		t.Logf("%s html=%d md=%d budget_ret=%d reduction_vs_md=%.1f%% selected=%v",
			c.File, htmlTok, mdTok, ret, reduction, res.Selected)
	}

	t.Log("\n" + strings.Join(rows, "\n"))
}

func TestBotChallengeNotCountedAsSavings(t *testing.T) {
	dir := fixtureDir(t)
	htmlBytes, err := os.ReadFile(filepath.Join(dir, "bot-challenge.html"))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(htmlBytes)
	}))
	defer ts.Close()

	ctr, _ := tokens.Default()
	eng := &gateway.Engine{Counter: ctr, Mode: convert.ModeReader, Client: ts.Client(), AllowPrivate: true}
	_, err = eng.ReadPage(context.Background(), ts.URL)
	if err == nil {
		t.Fatal("expected bot/challenge classification error")
	}
	// Important: caller must not compute token savings from this failure.
	t.Logf("challenge correctly rejected: %v", err)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [32]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func sprintf1(f float64) string {
	// one decimal without fmt to keep harness light
	n := int(f*10 + 0.5)
	return itoa(n/10) + "." + itoa(n%10)
}
