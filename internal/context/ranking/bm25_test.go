package ranking

import (
	"strings"
	"testing"

	"github.com/asmseo/mdcix/internal/document"
)

func TestBM25PrefersPricingSection(t *testing.T) {
	doc := document.Document{
		Sections: []document.Section{
			{ID: "intro", Title: "Welcome", Position: 0, Markdown: "Welcome to our product. Learn more about features and company culture."},
			{ID: "pricing", Title: "Pricing", Position: 1, Markdown: "Freelancer $29 /mo. Pro $79 /mo. Plus $99 /mo. Unlimited $199 /mo."},
			{ID: "faq", Title: "FAQ", Position: 2, Markdown: "Can we cancel anytime? Yes. Storage upgrades cost extra $50."},
		},
	}
	ranked := BM25(doc, "pricing plans monthly cost")
	if len(ranked) == 0 || ranked[0].Section.ID != "pricing" {
		t.Fatalf("expected pricing first, got %+v", ranked)
	}
}

func TestBM25RareEntityCoverage(t *testing.T) {
	doc := document.Document{
		Sections: []document.Section{
			{ID: "a", Title: "Overview", Position: 0, Markdown: strings.Repeat("cookie browser web tracking privacy policy history decades ", 40)},
			{ID: "b", Title: "History", Position: 1, Markdown: "Lou Montulli invented the HTTP cookie in 1994 while working at Netscape."},
			{ID: "c", Title: "See also", Position: 2, Markdown: "Related web standards and references."},
		},
	}
	ranked := BM25(doc, "Lou Montulli 1994")
	found := false
	for i, r := range ranked {
		if r.Section.ID == "b" && r.Score > 0 {
			found = true
			if i > 1 {
				t.Fatalf("history section ranked too low: %+v", ranked)
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected history section scored: %+v", ranked)
	}
}
