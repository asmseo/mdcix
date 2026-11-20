package links

import (
	"strings"
	"testing"
)

func TestFindPricingLink(t *testing.T) {
	html := `<!DOCTYPE html><html><body>
<nav>
  <a href="/features">Features</a>
  <a href="/pricing">Pricing</a>
  <a href="/login">Log in</a>
</nav>
<main>
  <a href="/blog/hello">Long blog about nothing related</a>
  <p>See our <a href="/pricing#plans">plans and pricing</a> today.</p>
</main>
</body></html>`
	res, err := Find(html, "https://basecamp.com/", Options{Query: "pricing plans", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalLinks < 4 {
		t.Fatalf("total=%d", res.TotalLinks)
	}
	if len(res.Matches) == 0 {
		t.Fatal("no matches")
	}
	top := res.Matches[0]
	if !strings.Contains(strings.ToLower(top.Href), "pricing") {
		t.Fatalf("top should be pricing, got %+v", top)
	}
}

func TestFindEmptyQueryCaps(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<html><body>`)
	for i := 0; i < 30; i++ {
		b.WriteString(`<a href="/p`)
		b.WriteByte(byte('a' + i%26))
		b.WriteString(`">Link</a>`)
	}
	b.WriteString(`</body></html>`)
	res, err := Find(b.String(), "https://example.com/", Options{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Returned != 10 {
		t.Fatalf("returned=%d", res.Returned)
	}
}
