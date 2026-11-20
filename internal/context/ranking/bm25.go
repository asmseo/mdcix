// Package ranking provides deterministic lexical section ranking (BM25).
package ranking

import (
	"math"
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
func BM25(doc document.Document, query string) []Result {
	q := tokenize(query)
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
	avgLen := 0.0
	docsTok := make([][]string, len(doc.Sections))
	for i, s := range doc.Sections {
		// Boost title/heading text
		text := s.Title + "\n" + s.Title + "\n" + s.Markdown
		toks := tokenize(text)
		docsTok[i] = toks
		avgLen += float64(len(toks))
	}
	avgLen /= N

	df := map[string]float64{}
	for _, toks := range docsTok {
		seen := map[string]struct{}{}
		for _, t := range toks {
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			df[t]++
		}
	}

	const k1 = 1.2
	const b = 0.75
	var results []Result
	for i, s := range doc.Sections {
		toks := docsTok[i]
		tf := map[string]float64{}
		for _, t := range toks {
			tf[t]++
		}
		dl := float64(len(toks))
		score := 0.0
		for _, term := range q {
			freq := tf[term]
			if freq == 0 {
				continue
			}
			n := df[term]
			idf := math.Log(1 + (N-n+0.5)/(n+0.5))
			denom := freq + k1*(1-b+b*dl/avgLen)
			score += idf * (freq * (k1 + 1) / denom)
			// exact title match boost
			if strings.EqualFold(s.Title, term) || strings.Contains(strings.ToLower(s.Title), term) {
				score *= 1.25
			}
		}
		results = append(results, Result{Section: s, Score: score})
	}
	// sort desc by score, stable by position
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score ||
				(results[j].Score == results[i].Score && results[j].Section.Position < results[i].Section.Position) {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
	return results
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
