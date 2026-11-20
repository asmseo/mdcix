// Package budget packs ranked sections into a token budget.
package budget

import (
	"strings"

	"github.com/asmseo/mdcix/internal/context/ranking"
	"github.com/asmseo/mdcix/internal/context/tokenizer"
	"github.com/asmseo/mdcix/internal/document"
	"github.com/asmseo/mdcix/internal/tokens"
)

// Request for budget-aware read.
type Request struct {
	Query  string
	Budget int
}

// Response is the packed selection result.
type Response struct {
	Markdown        string             `json:"markdown"`
	ReturnedTokens  int                `json:"returned_tokens"`
	RequestedBudget int                `json:"requested_budget"`
	Selected        []string           `json:"selected_sections"`
	Omitted         []string           `json:"omitted_sections"`
	Truncated       bool               `json:"truncated"`
	Warnings        []string           `json:"warnings,omitempty"`
	Attribution     string             `json:"source_attribution,omitempty"`
	DocumentVersion string             `json:"document_version"`
	URL             string             `json:"url"`
	Scores          map[string]float64 `json:"scores,omitempty"`
	// Metrics distinguish HTML→MD reduction from MD→selected-context reduction.
	// Task-level agent savings belong to the benchmark harness, not this struct.
	OriginalTokens          int     `json:"original_tokens,omitempty"`
	MarkdownTokens          int     `json:"markdown_tokens,omitempty"`
	ExtractionReductionPct  float64 `json:"extraction_reduction_pct,omitempty"`
	SelectionReductionPct   float64 `json:"selection_reduction_pct,omitempty"`
	TokenEstimateNote       string  `json:"token_estimate_note,omitempty"`
}

// Select packs BM25-ranked sections into budget without generative rewrite.
func Select(doc document.Document, req Request, ctr *tokens.Counter) Response {
	if ctr == nil {
		ctr, _ = tokens.Default()
		if ctr == nil {
			ctr = &tokens.Counter{}
		}
	}
	if req.Budget <= 0 {
		req.Budget = 1500
	}
	ranked := ranking.BM25(doc, req.Query)
	resp := Response{
		RequestedBudget: req.Budget,
		DocumentVersion: doc.Version,
		URL:             doc.URL,
		Attribution:     doc.URL,
		Warnings:        append([]string{}, doc.Warnings...),
		Scores:          map[string]float64{},
		OriginalTokens:  doc.OriginalTokens,
		MarkdownTokens:  doc.MarkdownTokens,
		TokenEstimateNote: tokenizer.Note,
	}
	resp.ExtractionReductionPct = pctReduction(doc.OriginalTokens, doc.MarkdownTokens)
	if req.Query != "" && looksCrossLanguage(req.Query, doc) {
		resp.Warnings = append(resp.Warnings, "possible_cross_language_query: BM25 is lexical; prefer agent-selected outline/sections")
	}

	var parts []string
	used := 0
	selected := map[string]struct{}{}
	// Include parent context once if child selected and parent not yet in
	for _, r := range ranked {
		if r.Score <= 0 && req.Query != "" {
			resp.Omitted = append(resp.Omitted, r.Section.ID)
			continue
		}
		sec := r.Section
		chunk := sec.Markdown
		cost := ctr.Count(chunk)
		if used+cost > req.Budget {
			// try truncated section if nothing selected yet
			if used == 0 {
				trunc := truncateToBudget(chunk, req.Budget, ctr)
				parts = append(parts, trunc)
				used = ctr.Count(trunc)
				selected[sec.ID] = struct{}{}
				resp.Scores[sec.ID] = r.Score
				resp.Truncated = true
				resp.Warnings = append(resp.Warnings, "section_truncated_to_fit_budget:"+sec.ID)
			} else {
				resp.Omitted = append(resp.Omitted, sec.ID)
				resp.Truncated = true
			}
			continue
		}
		parts = append(parts, chunk)
		used += cost
		selected[sec.ID] = struct{}{}
		resp.Scores[sec.ID] = r.Score
	}
	// restore document order for selected
	var ordered []string
	var orderedIDs []string
	for _, s := range doc.Sections {
		if _, ok := selected[s.ID]; ok {
			// find part by rebuilding from section markdown to keep integrity
			ordered = append(ordered, s.Markdown)
			orderedIDs = append(orderedIDs, s.ID)
		} else if req.Query == "" || (len(ranked) > 0) {
			// already in omitted if scored; ensure all non-selected listed
			skip := false
			for _, id := range resp.Omitted {
				if id == s.ID {
					skip = true
					break
				}
			}
			if !skip {
				if _, ok := selected[s.ID]; !ok {
					resp.Omitted = append(resp.Omitted, s.ID)
				}
			}
		}
	}
	if len(ordered) > 0 {
		resp.Markdown = strings.Join(ordered, "\n\n")
		resp.Selected = orderedIDs
		resp.ReturnedTokens = ctr.Count(resp.Markdown)
	} else if len(parts) > 0 {
		resp.Markdown = strings.Join(parts, "\n\n")
		for id := range selected {
			resp.Selected = append(resp.Selected, id)
		}
		resp.ReturnedTokens = ctr.Count(resp.Markdown)
	}
	if len(resp.Selected) == 0 {
		resp.Warnings = append(resp.Warnings, "no_sections_selected")
	}
	resp.SelectionReductionPct = pctReduction(doc.MarkdownTokens, resp.ReturnedTokens)
	return resp
}

// PackSections packs explicit sections in doc order into budget.
func PackSections(doc document.Document, ids []string, budget int, ctr *tokens.Counter) Response {
	if ctr == nil {
		ctr, _ = tokens.Default()
	}
	if budget <= 0 {
		budget = 1 << 30
	}
	secs, missing := doc.SectionsByIDs(ids)
	resp := Response{
		RequestedBudget:       budget,
		DocumentVersion:       doc.Version,
		URL:                   doc.URL,
		Attribution:           doc.URL,
		Warnings:              append([]string{}, doc.Warnings...),
		OriginalTokens:        doc.OriginalTokens,
		MarkdownTokens:        doc.MarkdownTokens,
		TokenEstimateNote:     tokenizer.Note,
		ExtractionReductionPct: pctReduction(doc.OriginalTokens, doc.MarkdownTokens),
	}
	for _, id := range missing {
		resp.Warnings = append(resp.Warnings, "missing_section:"+id)
		resp.Omitted = append(resp.Omitted, id)
	}
	used := 0
	var parts []string
	for _, s := range secs {
		cost := s.Tokens
		if cost == 0 {
			cost = ctr.Count(s.Markdown)
		}
		if used+cost > budget {
			resp.Omitted = append(resp.Omitted, s.ID)
			resp.Truncated = true
			continue
		}
		parts = append(parts, s.Markdown)
		resp.Selected = append(resp.Selected, s.ID)
		used += cost
	}
	resp.Markdown = strings.Join(parts, "\n\n")
	resp.ReturnedTokens = ctr.Count(resp.Markdown)
	resp.SelectionReductionPct = pctReduction(doc.MarkdownTokens, resp.ReturnedTokens)
	return resp
}

func pctReduction(from, to int) float64 {
	if from <= 0 {
		return 0
	}
	saved := from - to
	if saved < 0 {
		saved = 0
	}
	return float64(saved) / float64(from) * 100
}

func truncateToBudget(md string, budget int, ctr *tokens.Counter) string {
	if ctr.Count(md) <= budget {
		return md
	}
	// binary search by rune length
	r := []rune(md)
	lo, hi := 0, len(r)
	best := ""
	for lo <= hi {
		mid := (lo + hi) / 2
		cand := string(r[:mid])
		if ctr.Count(cand) <= budget {
			best = cand
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	if best == "" {
		return ""
	}
	return strings.TrimSpace(best) + "\n\n<!-- truncated -->"
}

func looksCrossLanguage(query string, doc document.Document) bool {
	// Heuristic: script / accent mismatch — Portuguese/Spanish docs with ASCII-only English query is OK;
	// flag when query has Latin accents and body doesn't, or opposite with CJK, etc.
	qHas := hasAccent(query)
	body := doc.Title + " " + doc.Markdown
	if len(body) > 2000 {
		body = body[:2000]
	}
	bHas := hasAccent(body)
	if qHas != bHas && (qHas || bHas) {
		return true
	}
	return false
}

func hasAccent(s string) bool {
	for _, r := range s {
		if r > 127 && r < 0x3000 {
			return true
		}
	}
	return false
}
