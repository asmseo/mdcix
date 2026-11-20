package convert

import (
	"strings"
	"testing"
)

const sampleHTML = `<!DOCTYPE html>
<html>
<head><title>Demo</title><script>alert(1)</script><style>.x{}</style></head>
<body>
<nav><a href="/home">Home</a></nav>
<article>
  <h1>Hello World</h1>
  <p>This is the <strong>main</strong> article content with enough text to pass the threshold.</p>
  <pre><code class="language-go">package main
func main() {}
</code></pre>
  <table><tr><th>A</th><th>B</th></tr><tr><td>1</td><td>2</td></td></tr></table>
  <p><img alt="sprite" src="data:image/png;base64,aaaa"></p>
  <p><a href="/docs">Docs</a> and <a href="https://example.com/x">External</a></p>
</article>
<footer>Copyright</footer>
</body>
</html>`

func TestHTMLToMarkdownReader(t *testing.T) {
	res, err := HTMLToMarkdown(sampleHTML, Options{Mode: ModeReader, BaseURL: "https://example.com/page"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Source != "converted" {
		t.Fatalf("source=%s", res.Source)
	}
	if !strings.Contains(res.Markdown, "Hello World") {
		t.Fatalf("missing title: %s", res.Markdown)
	}
	if !strings.Contains(res.Markdown, "main") {
		t.Fatalf("missing body: %s", res.Markdown)
	}
	if strings.Contains(res.Markdown, "alert(1)") || strings.Contains(res.Markdown, "Copyright") {
		t.Fatalf("noise leaked: %s", res.Markdown)
	}
	if strings.Contains(res.Markdown, "data:image") {
		t.Fatalf("data URI not cleaned: %s", res.Markdown)
	}
	// Reader mode should not append the Links section.
	if strings.Contains(res.Markdown, "## Links") {
		t.Fatalf("reader should not have Links section")
	}
}

func TestHTMLToMarkdownAgent(t *testing.T) {
	res, err := HTMLToMarkdown(sampleHTML, Options{Mode: ModeAgent, BaseURL: "https://example.com/page"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Markdown, "## Links") {
		t.Fatalf("agent should append Links: %s", res.Markdown)
	}
	if !strings.Contains(res.Markdown, "https://example.com/docs") {
		t.Fatalf("relative link not absolutized: %s", res.Markdown)
	}
}

func TestIsHTMLIsMarkdown(t *testing.T) {
	if !IsHTML("text/html; charset=utf-8") {
		t.Fatal("html")
	}
	if !IsMarkdown("text/markdown; charset=utf-8") {
		t.Fatal("md")
	}
	if IsHTML("application/json") {
		t.Fatal("json not html")
	}
}

func TestPassthrough(t *testing.T) {
	r := Passthrough("  # Hi\n")
	if r.Source != "native" || r.Markdown != "# Hi" {
		t.Fatalf("%+v", r)
	}
}

func TestCleanupDataURI(t *testing.T) {
	in := "Before\n\n![x](data:image/png;base64,abc)\n\nAfter"
	out := cleanupMarkdown(in)
	if strings.Contains(out, "data:") {
		t.Fatal(out)
	}
}

func TestExtractPrefersMainOverTinyArticle(t *testing.T) {
	html := `<!DOCTYPE html><html><body>
<article>Thousands of people are working right now.</article>
<main>
  <h1>Pricing</h1>
  <p>Freelancer $29 /mo. Pro $79 /mo. Plus $99 /mo. Unlimited $199 /mo.</p>
  <p>` + strings.Repeat("More product detail about packages and storage. ", 40) + `</p>
</main>
</body></html>`
	res, err := HTMLToMarkdown(html, Options{Mode: ModeReader})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"$29", "$79", "$99", "$199"} {
		if !strings.Contains(res.Markdown, want) {
			t.Fatalf("missing %s in: %s", want, res.Markdown)
		}
	}
}

func TestPlainStripsLinks(t *testing.T) {
	html := `<!DOCTYPE html><html><body><article>
  <h1>Hello</h1>
  <p>See <a href="https://example.com/docs">the docs</a> for details and more text here.</p>
</article></body></html>`
	res, err := HTMLToMarkdown(html, Options{Mode: ModeReader, Plain: true, BaseURL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Markdown, "https://") || strings.Contains(res.Markdown, "](") {
		t.Fatalf("plain should strip urls: %s", res.Markdown)
	}
	if !strings.Contains(res.Markdown, "the docs") {
		t.Fatalf("should keep label: %s", res.Markdown)
	}
}
