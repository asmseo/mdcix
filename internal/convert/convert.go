// Package convert extracts main content from HTML and converts it to Markdown.
//
// Modes:
//   - reader: article/main content only (maximum token savings)
//   - agent:  article plus useful links (better for agent navigation)
//
// Plain (opt-in): strip markdown links/images to bare text while keeping structure.
//
// Ideas inspired by md-browse (Accept preference / cleanup) and webforai
// (reader vs agent). All code here is original MIT.
package convert

import (
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/PuerkitoBio/goquery"
)

// Mode selects extraction aggressiveness.
type Mode string

const (
	ModeReader Mode = "reader"
	ModeAgent  Mode = "agent"
)

// Options configures conversion.
type Options struct {
	Mode    Mode
	BaseURL string // resolve relative links when set
	Plain   bool   // strip markdown links/images to text (opt-in; default keeps navigational links)
}

// Result is the converted Markdown plus provenance.
type Result struct {
	Markdown string
	Source   string // "native" | "converted"
}

var (
	dataURIImage = regexp.MustCompile(`!\[[^\]]*\]\(data:[^)]+\)`)
	dataURILink  = regexp.MustCompile(`\[[^\]]*\]\(data:[^)]+\)`)
	multiBlank   = regexp.MustCompile(`\n{3,}`)
	emptyListNum = regexp.MustCompile(`(?m)^\d+\.\s*$`)
	mdLink       = regexp.MustCompile(`\[([^\]]*)\]\(([^)]+)\)`)
	mdImage      = regexp.MustCompile(`!\[[^\]]*\]\([^)]+\)`)
)

// IsHTML reports whether Content-Type looks like HTML.
func IsHTML(ct string) bool {
	ct = strings.ToLower(ct)
	return strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml")
}

// IsMarkdown reports whether Content-Type looks like Markdown.
func IsMarkdown(ct string) bool {
	ct = strings.ToLower(ct)
	return strings.Contains(ct, "text/markdown") || strings.Contains(ct, "text/x-markdown")
}

// HTMLToMarkdown extracts content and converts HTML to Markdown.
func HTMLToMarkdown(html string, opts Options) (Result, error) {
	if opts.Mode == "" {
		opts.Mode = ModeReader
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return Result{}, fmt.Errorf("parse html: %w", err)
	}

	stripNoise(doc)
	fragment := extractContent(doc, opts.Mode)

	if opts.BaseURL != "" {
		absolutizeLinks(fragment, opts.BaseURL)
	}

	rawHTML, err := fragment.Html()
	if err != nil {
		return Result{}, fmt.Errorf("serialize fragment: %w", err)
	}

	md, err := htmltomarkdown.ConvertString(rawHTML)
	if err != nil {
		return Result{}, fmt.Errorf("html-to-markdown: %w", err)
	}

	if opts.Mode == ModeAgent && !opts.Plain {
		md = appendUsefulLinks(md, doc, opts.BaseURL)
	}

	md = cleanupMarkdown(md)
	if opts.Plain {
		md = StripLinks(md)
	}
	return Result{Markdown: md, Source: "converted"}, nil
}

// Passthrough wraps already-markdown body.
func Passthrough(body string) Result {
	return Result{Markdown: strings.TrimSpace(body), Source: "native"}
}

// StripLinks removes markdown links/images, keeping link labels (navigational URLs gone).
func StripLinks(md string) string {
	md = mdImage.ReplaceAllString(md, "")
	md = mdLink.ReplaceAllString(md, "$1")
	md = multiBlank.ReplaceAllString(md, "\n\n")
	return strings.TrimSpace(md)
}

func stripNoise(doc *goquery.Document) {
	doc.Find("script, style, noscript, iframe, object, embed, meta, link[rel='stylesheet'], svg, canvas").Remove()
	doc.Find("nav, footer, header, aside, [role='navigation'], [role='banner'], [role='contentinfo']").Remove()
	doc.Find(".nav, .navbar, .footer, .sidebar, .cookie, .advertisement, .ads, #cookie-banner").Remove()
}

func extractContent(doc *goquery.Document, mode Mode) *goquery.Selection {
	_ = mode
	type cand struct {
		sel  *goquery.Selection
		text string
		n    int
	}
	selectors := []string{
		"main",
		"[role='main']",
		"article",
		"#content",
		"#main",
		".post-content",
		".article-body",
		".entry-content",
	}
	var best cand
	for _, sel := range selectors {
		doc.Find(sel).Each(func(_ int, node *goquery.Selection) {
			text := strings.TrimSpace(node.Text())
			n := len([]rune(text))
			if n <= 40 {
				return
			}
			if n > best.n {
				best = cand{sel: node, text: text, n: n}
			}
		})
	}

	body := doc.Find("body")
	bodyText := ""
	bodyN := 0
	if body.Length() > 0 {
		bodyText = strings.TrimSpace(body.Text())
		bodyN = len([]rune(bodyText))
	}

	// Prefer best semantic region when it covers a meaningful share of the body.
	// Tiny first-<article> teaser (e.g. Basecamp promo blurb) must not win over <main>.
	if best.sel != nil && best.n > 0 {
		if bodyN == 0 || best.n*5 >= bodyN || best.n >= 800 {
			return best.sel
		}
	}
	if body.Length() > 0 {
		return body
	}
	return doc.Selection
}

func absolutizeLinks(sel *goquery.Selection, base string) {
	baseURL, err := url.Parse(base)
	if err != nil {
		return
	}
	sel.Find("a[href], img[src]").Each(func(_ int, s *goquery.Selection) {
		attr := "href"
		if s.Is("img") {
			attr = "src"
		}
		val, ok := s.Attr(attr)
		if !ok || val == "" || strings.HasPrefix(val, "data:") {
			return
		}
		if strings.HasPrefix(val, "//") {
			s.SetAttr(attr, "https:"+val)
			return
		}
		ref, err := url.Parse(val)
		if err != nil {
			return
		}
		s.SetAttr(attr, baseURL.ResolveReference(ref).String())
	})
}

func appendUsefulLinks(md string, doc *goquery.Document, base string) string {
	seen := map[string]struct{}{}
	var links []string
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		href = strings.TrimSpace(href)
		if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "javascript:") || strings.HasPrefix(href, "data:") {
			return
		}
		if strings.HasPrefix(href, "//") {
			href = "https:" + href
		}
		if base != "" {
			if u, err := url.Parse(base); err == nil {
				if ref, err := url.Parse(href); err == nil {
					href = u.ResolveReference(ref).String()
				}
			}
		}
		label := normalizeWS(s.Text())
		if label == "" {
			label = href
		}
		key := href + "|" + label
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		links = append(links, fmt.Sprintf("- [%s](%s)", label, href))
	})
	if len(links) == 0 {
		return md
	}
	const maxLinks = 40
	if len(links) > maxLinks {
		links = links[:maxLinks]
	}
	var buf bytes.Buffer
	buf.WriteString(md)
	buf.WriteString("\n\n## Links\n\n")
	buf.WriteString(strings.Join(links, "\n"))
	buf.WriteByte('\n')
	return buf.String()
}

func cleanupMarkdown(md string) string {
	md = dataURIImage.ReplaceAllString(md, "")
	md = dataURILink.ReplaceAllString(md, "")
	md = emptyListNum.ReplaceAllString(md, "")
	md = mdLink.ReplaceAllStringFunc(md, func(m string) string {
		parts := mdLink.FindStringSubmatch(m)
		if len(parts) != 3 {
			return m
		}
		label := normalizeWS(parts[1])
		u := strings.TrimSpace(parts[2])
		if strings.HasPrefix(u, "//") {
			u = "https:" + u
		}
		if label == "" {
			label = u
		}
		if u == "" {
			return label
		}
		return fmt.Sprintf("[%s](%s)", label, u)
	})
	md = multiBlank.ReplaceAllString(md, "\n\n")
	return strings.TrimSpace(md)
}

func normalizeWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
