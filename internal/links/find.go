// Package links finds and ranks hyperlinks for agent navigation without
// loading full page content into the model context.
package links

import (
	"net/url"
	"sort"
	"strings"
	"unicode"

	"github.com/PuerkitoBio/goquery"
)

// Match is one ranked hyperlink.
type Match struct {
	Text  string  `json:"text"`
	Href  string  `json:"href"`
	Score float64 `json:"score"`
	Title string  `json:"title,omitempty"` // title/aria-label when useful
}

// Result is a compact link search response.
type Result struct {
	URL         string  `json:"url"`
	FinalURL    string  `json:"final_url,omitempty"`
	Query       string  `json:"query"`
	TotalLinks  int     `json:"total_links"`
	Returned    int     `json:"returned"`
	Matches     []Match `json:"matches"`
	Warnings    []string `json:"warnings,omitempty"`
	JSRendered  bool    `json:"js_rendered,omitempty"`
}

// Options configure Find.
type Options struct {
	Query string
	Limit int
	// IncludeSamePageHash keeps #fragment-only links (default false).
	IncludeSamePageHash bool
}

// Find extracts <a href> from HTML (including nav/header — not content-extracted)
// and ranks them against query. Empty query returns links in document order (capped).
func Find(html, pageURL string, opts Options) (Result, error) {
	if opts.Limit <= 0 {
		opts.Limit = 15
	}
	res := Result{URL: pageURL, FinalURL: pageURL, Query: opts.Query}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return res, err
	}

	base, _ := url.Parse(pageURL)
	seen := map[string]struct{}{}
	var all []Match

	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		href = strings.TrimSpace(href)
		if href == "" || strings.HasPrefix(strings.ToLower(href), "javascript:") ||
			strings.HasPrefix(strings.ToLower(href), "mailto:") ||
			strings.HasPrefix(strings.ToLower(href), "tel:") ||
			strings.HasPrefix(href, "data:") {
			return
		}
		if !opts.IncludeSamePageHash && strings.HasPrefix(href, "#") {
			return
		}
		abs := absolutize(base, href)
		if abs == "" {
			return
		}
		text := normalizeWS(s.Text())
		title, _ := s.Attr("title")
		if title == "" {
			title, _ = s.Attr("aria-label")
		}
		title = normalizeWS(title)
		if text == "" {
			text = title
		}
		if text == "" {
			// last resort: last path segment
			if u, err := url.Parse(abs); err == nil {
				parts := strings.Split(strings.Trim(u.Path, "/"), "/")
				if len(parts) > 0 && parts[len(parts)-1] != "" {
					text = parts[len(parts)-1]
				} else {
					text = abs
				}
			}
		}
		key := strings.ToLower(abs) + "|" + strings.ToLower(text)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		all = append(all, Match{Text: text, Href: abs, Title: title})
	})
	res.TotalLinks = len(all)

	q := tokenize(opts.Query)
	if len(q) == 0 {
		if len(all) > opts.Limit {
			all = all[:opts.Limit]
		}
		res.Matches = all
		res.Returned = len(all)
		return res, nil
	}

	for i := range all {
		all[i].Score = scoreLink(all[i], q)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Score == all[j].Score {
			return len(all[i].Text) < len(all[j].Text)
		}
		return all[i].Score > all[j].Score
	})

	var ranked []Match
	for _, m := range all {
		if m.Score <= 0 {
			continue
		}
		ranked = append(ranked, m)
		if len(ranked) >= opts.Limit {
			break
		}
	}
	// If nothing scored, return top lexical-ish path matches diluted — still useful for "pricing" in href.
	if len(ranked) == 0 {
		for _, m := range all {
			if hrefHasQueryToken(m.Href, q) {
				m.Score = 0.1
				ranked = append(ranked, m)
				if len(ranked) >= opts.Limit {
					break
				}
			}
		}
	}
	res.Matches = ranked
	res.Returned = len(ranked)
	return res, nil
}

func scoreLink(m Match, q []string) float64 {
	hay := strings.ToLower(m.Text + " " + m.Title + " " + m.Href)
	hayToks := tokenize(hay)
	set := map[string]struct{}{}
	for _, t := range hayToks {
		set[t] = struct{}{}
	}
	score := 0.0
	hits := 0
	for _, term := range q {
		if _, ok := set[term]; ok {
			hits++
			score += 1.0
			// exact text equality / contains as phrase-ish
			if strings.EqualFold(m.Text, term) {
				score += 2.0
			} else if strings.Contains(strings.ToLower(m.Text), term) {
				score += 1.2
			}
			if strings.Contains(strings.ToLower(m.Href), term) {
				score += 0.8
			}
		}
	}
	if hits == len(q) && len(q) > 1 {
		score *= 1.35
	}
	// Prefer shorter nav labels over long paragraph links when scores close — applied via sort tiebreak.
	return score
}

func hrefHasQueryToken(href string, q []string) bool {
	low := strings.ToLower(href)
	for _, t := range q {
		if strings.Contains(low, t) {
			return true
		}
	}
	return false
}

func absolutize(base *url.URL, href string) string {
	if strings.HasPrefix(href, "//") {
		return "https:" + href
	}
	ref, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if base == nil {
		return href
	}
	return base.ResolveReference(ref).String()
}

func normalizeWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func tokenize(s string) []string {
	s = strings.ToLower(s)
	var b strings.Builder
	var out []string
	flush := func() {
		if b.Len() == 0 {
			return
		}
		out = append(out, b.String())
		b.Reset()
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}
