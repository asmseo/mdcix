// Package ranking provides deterministic lexical section ranking (BM25).
package ranking

import (
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/asmseo/mdcix/internal/document"
)

// Result is a ranked section with score.
type Result struct {
	Section document.Section
	Score   float64
}

// BM25 ranks document sections for a query (language-dependent lexical).
//
// Enhancements over plain BM25:
//   - query synonym expansion for pricing / plan vocabulary (PT/EN/ES light)
//   - heavier title/heading field boost
//   - rare-term / numeric entity boost (years, prices, proper names in query)
//   - soft coverage: sections that uniquely hold a rare query term get a floor score
func BM25(doc document.Document, query string) []Result {
	q := tokenize(expandQuery(query))
	if len(q) == 0 {
		out := make([]Result, 0, len(doc.Sections))
		for _, s := range doc.Sections {
			out = append(out, Result{Section: s, Score: 0})
		}
		return out
	}

	N := float64(len(doc.Sections))
	if N == 0 {
		return nil
	}

	type fields struct {
		title []string
		body  []string
		all   []string
	}
	docs := make([]fields, len(doc.Sections))
	avgLen := 0.0
	for i, s := range doc.Sections {
		titleToks := tokenize(s.Title)
		bodyToks := tokenize(s.Markdown)
		all := make([]string, 0, len(titleToks)*3+len(bodyToks))
		// title tripled into the bag for classic field boost
		all = append(all, titleToks...)
		all = append(all, titleToks...)
		all = append(all, titleToks...)
		all = append(all, bodyToks...)
		docs[i] = fields{title: titleToks, body: bodyToks, all: all}
		avgLen += float64(len(all))
	}
	avgLen /= N

	df := map[string]float64{}
	for _, d := range docs {
		seen := map[string]struct{}{}
		for _, t := range d.all {
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			df[t]++
		}
	}

	// Sections that uniquely (or rarely) contain a query term — coverage floor.
	termOwners := map[string][]int{}
	for i, d := range docs {
		seen := map[string]struct{}{}
		for _, t := range d.all {
			seen[t] = struct{}{}
		}
		for _, term := range unique(q) {
			if _, ok := seen[term]; ok {
				termOwners[term] = append(termOwners[term], i)
			}
		}
	}

	const k1 = 1.2
	const b = 0.75
	results := make([]Result, len(doc.Sections))
	for i, s := range doc.Sections {
		d := docs[i]
		tf := map[string]float64{}
		for _, t := range d.all {
			tf[t]++
		}
		titleSet := map[string]struct{}{}
		for _, t := range d.title {
			titleSet[t] = struct{}{}
		}
		dl := float64(len(d.all))
		score := 0.0
		for _, term := range q {
			freq := tf[term]
			if freq == 0 {
				continue
			}
			n := df[term]
			idf := math.Log(1 + (N-n+0.5)/(n+0.5))
			denom := freq + k1*(1-b+b*dl/avgLen)
			contrib := idf * (freq * (k1 + 1) / denom)
			if _, ok := titleSet[term]; ok {
				contrib *= 1.6
			}
			if isRareOrEntity(term, n, N) {
				contrib *= 1.8
			}
			score += contrib
		}
		// Phrase / multi-term: if most query terms appear, small cohesion bonus.
		if len(unique(q)) >= 2 {
			hit := 0
			for _, term := range unique(q) {
				if tf[term] > 0 {
					hit++
				}
			}
			if hit == len(unique(q)) {
				score *= 1.15
			}
		}
		results[i] = Result{Section: s, Score: score}
	}

	// Coverage floor: rare query terms must surface their owning sections.
	for term, owners := range termOwners {
		n := df[term]
		if !isRareOrEntity(term, n, N) {
			continue
		}
		if len(owners) == 0 || len(owners) > 3 {
			continue
		}
		for _, i := range owners {
			if results[i].Score < 0.5 {
				results[i].Score = 0.5
			} else {
				results[i].Score *= 1.1
			}
		}
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].Section.Position < results[j].Section.Position
		}
		return results[i].Score > results[j].Score
	})
	return results
}

func expandQuery(q string) string {
	low := strings.ToLower(q)
	extra := make([]string, 0, 8)
	add := func(words ...string) {
		for _, w := range words {
			if !strings.Contains(low, w) {
				extra = append(extra, w)
			}
		}
	}
	switch {
	case strings.Contains(low, "preço") || strings.Contains(low, "precos") || strings.Contains(low, "preços") ||
		strings.Contains(low, "price") || strings.Contains(low, "pricing") || strings.Contains(low, "cost") ||
		strings.Contains(low, "mensal") || strings.Contains(low, "monthly") || strings.Contains(low, "plano") ||
		strings.Contains(low, "plan") || strings.Contains(low, "assinatura") || strings.Contains(low, "subscription"):
		add("price", "pricing", "plan", "plans", "monthly", "preço", "precos", "plano", "planos", "mensal")
	}
	if len(extra) == 0 {
		return q
	}
	return q + " " + strings.Join(extra, " ")
}

func isRareOrEntity(term string, df, nDocs float64) bool {
	if term == "" {
		return false
	}
	// numeric / year / price-like
	digits := 0
	for _, r := range term {
		if unicode.IsDigit(r) {
			digits++
		}
	}
	if digits > 0 && digits >= len(term)/2 {
		return true
	}
	if len(term) >= 6 && df <= math.Max(1, nDocs*0.15) {
		return true
	}
	return df <= 1
}

func unique(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
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
