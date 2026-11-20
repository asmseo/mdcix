package document

import (
	"strings"
	"testing"

	"github.com/asmseo/mdcix/internal/tokens"
)

func TestSegmentHeadingsStableIDs(t *testing.T) {
	ctr, err := tokens.New("cl100k_base")
	if err != nil {
		t.Fatal(err)
	}
	md := `# Title

Intro.

## Pricing

Plans.

## Pricing

Duplicate.

## Integrations

Hooks.
`
	doc := BuildFromMarkdown(md, Document{URL: "https://ex.com"}, ctr)
	if len(doc.Sections) < 3 {
		t.Fatalf("sections=%d", len(doc.Sections))
	}
	ids := map[string]int{}
	for _, s := range doc.Sections {
		ids[s.ID]++
		if ids[s.ID] > 1 {
			t.Fatalf("duplicate id %s", s.ID)
		}
	}
	// duplicate heading should get -2
	foundDup := false
	for _, s := range doc.Sections {
		if strings.HasPrefix(s.ID, "pricing") && s.ID != "pricing" {
			foundDup = true
		}
	}
	if !foundDup {
		t.Fatalf("expected disambiguated pricing id: %+v", doc.Sections)
	}
	if doc.Version == "" || doc.ContentHash == "" {
		t.Fatal("missing version/hash")
	}
	o := doc.ToOutline(40)
	if o.TotalTokens != doc.MarkdownTokens || len(o.Sections) != len(doc.Sections) {
		t.Fatalf("outline mismatch: %+v", o)
	}
}

func TestSegmentUnstructured(t *testing.T) {
	ctr, _ := tokens.New("cl100k_base")
	md := `Short line

` + strings.Repeat("Paragraph about widgets and pricing details. ", 40) + `

` + strings.Repeat("Another block about integrations and APIs. ", 40)
	doc := BuildFromMarkdown(md, Document{URL: "https://ex.com"}, ctr)
	if len(doc.Sections) < 1 {
		t.Fatal("expected sections")
	}
	for _, s := range doc.Sections {
		if !s.Inferred {
			t.Fatalf("expected inferred: %+v", s)
		}
	}
}

func TestSectionsByIDsOrder(t *testing.T) {
	ctr, _ := tokens.New("cl100k_base")
	doc := BuildFromMarkdown("# A\n\nx\n\n## B\n\ny\n\n## C\n\nz\n", Document{URL: "u"}, ctr)
	secs, missing := doc.SectionsByIDs([]string{"c", "a", "nope"})
	if len(missing) != 1 || missing[0] != "nope" {
		t.Fatalf("missing=%v", missing)
	}
	if len(secs) != 2 || secs[0].ID != "a" || secs[1].ID != "c" {
		t.Fatalf("order=%v", secs)
	}
}
