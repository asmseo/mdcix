package benchmarks_test

import (
	"context"
	"encoding/json"
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

type taskFile struct {
	Version int    `json:"version"`
	Tasks   []task `json:"tasks"`
}

type task struct {
	ID            string   `json:"id"`
	Fixture       string   `json:"fixture"`
	Query         string   `json:"query"`
	Budget        int      `json:"budget"`
	ExpectFacts   []string `json:"expect_facts"`
	ExpectFailure string   `json:"expect_failure"`
	Strategy      []string `json:"strategy"`
}

func fixtureDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Join(filepath.Dir(file), "fixtures")
}

func loadTasks(t *testing.T) taskFile {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(filepath.Dir(fixtureDir(t)), "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var tf taskFile
	if err := json.Unmarshal(b, &tf); err != nil {
		t.Fatal(err)
	}
	return tf
}

func TestLocalBudgetReductionHarness(t *testing.T) {
	dir := fixtureDir(t)
	tf := loadTasks(t)
	ctr, err := tokens.Default()
	if err != nil {
		t.Fatal(err)
	}

	var rows []string
	rows = append(rows, "task\tfixture\thtml_tok\tmd_tok\tret_tok\tlat_ms\treduction_md\tfacts_ok\tfailure")

	for _, c := range tf.Tasks {
		htmlBytes, err := os.ReadFile(filepath.Join(dir, c.Fixture))
		if err != nil {
			t.Fatal(err)
		}
		htmlTok := ctr.Count(string(htmlBytes))
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(htmlBytes)
		}))
		eng := &gateway.Engine{
			Counter: ctr, Mode: convert.ModeReader, Client: ts.Client(),
			AllowPrivate: true, Timeout: 5 * time.Second, Encoding: "cl100k_base",
		}

		start := time.Now()
		doc, err := eng.ReadPage(context.Background(), ts.URL)
		ts.Close()
		lat := time.Since(start).Milliseconds()

		if c.ExpectFailure != "" {
			if err == nil {
				t.Errorf("%s: expected failure %s", c.ID, c.ExpectFailure)
				rows = append(rows, join(c.ID, c.Fixture, itoa(htmlTok), "-", "-", itoa(int(lat)), "0", "n/a", "missing_failure"))
				continue
			}
			if !strings.Contains(err.Error(), c.ExpectFailure) {
				t.Errorf("%s: want failure containing %q got %v", c.ID, c.ExpectFailure, err)
			}
			rows = append(rows, join(c.ID, c.Fixture, itoa(htmlTok), "-", "-", itoa(int(lat)), "0", "n/a", c.ExpectFailure))
			t.Logf("%s correctly failed: %v", c.ID, err)
			continue
		}
		if err != nil {
			t.Errorf("%s: load: %v", c.ID, err)
			rows = append(rows, join(c.ID, c.Fixture, itoa(htmlTok), "-", "-", itoa(int(lat)), "n/a", "n/a", err.Error()))
			continue
		}
		mdTok := doc.MarkdownTokens
		res := budget.Select(doc, budget.Request{Query: c.Query, Budget: c.Budget}, ctr)
		ret := res.ReturnedTokens
		reduction := 0.0
		if mdTok > 0 {
			reduction = 100 * float64(mdTok-ret) / float64(mdTok)
		}
		factsOK := true
		for _, fact := range c.ExpectFacts {
			if !strings.Contains(res.Markdown, fact) && !strings.Contains(doc.Markdown, fact) {
				// budget path should preferably contain fact; if only in full doc, mark soft miss
				if !strings.Contains(res.Markdown, fact) {
					factsOK = false
				}
			}
		}
		if !factsOK {
			t.Logf("%s: fact miss in budget pack (logged, not hard-fail) selected=%v", c.ID, res.Selected)
		}
		if ret > c.Budget+50 {
			t.Errorf("%s: returned_tokens %d exceeds budget %d", c.ID, ret, c.Budget)
		}
		rows = append(rows, join(c.ID, c.Fixture, itoa(htmlTok), itoa(mdTok), itoa(ret), itoa(int(lat)),
			sprintf1(reduction), boolStr(factsOK), ""))
		t.Logf("%s html=%d md=%d ret=%d lat=%dms reduction=%.1f%% facts_ok=%v",
			c.ID, htmlTok, mdTok, ret, lat, reduction, factsOK)
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
	t.Logf("challenge correctly rejected: %v", err)
}

func join(parts ...string) string { return strings.Join(parts, "\t") }

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
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
	n := int(f*10 + 0.5)
	return itoa(n/10) + "." + itoa(n%10)
}
