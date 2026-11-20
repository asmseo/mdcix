package policy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEvaluateRobotsDisallowWarning(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	c := &Checker{Enabled: true, Client: ts.Client(), UserAgent: "mdcix-test"}
	d := c.Evaluate(context.Background(), ts.URL+"/page")
	if !d.Allow {
		t.Fatal("local checker fail-opens")
	}
	if d.Robots != "disallowed" {
		t.Fatalf("robots=%s", d.Robots)
	}
	if len(d.Warnings) == 0 {
		t.Fatal("expected warnings")
	}
}

func TestDisabled(t *testing.T) {
	d := (&Checker{Enabled: false}).Evaluate(context.Background(), "https://example.com/x")
	if !d.Allow {
		t.Fatal("expected allow")
	}
}
