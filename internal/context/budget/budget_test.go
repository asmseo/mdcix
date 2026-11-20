package budget

import (
	"strings"
	"testing"

	"github.com/asmseo/mdcix/internal/document"
	"github.com/asmseo/mdcix/internal/tokens"
)

func TestSelectBudget(t *testing.T) {
	ctr, err := tokens.New("cl100k_base")
	if err != nil {
		t.Fatal(err)
	}
	md := `# Product

## Pricing

Enterprise plans cost money and include support.

## Integrations

Connect Slack, GitHub, and webhooks for automation.

## History

Old news nobody needs for pricing questions.
`
	doc := document.BuildFromMarkdown(md, document.Document{URL: "https://ex.com"}, ctr)
	res := Select(doc, Request{Query: "pricing plans", Budget: 200}, ctr)
	if res.ReturnedTokens <= 0 || res.ReturnedTokens > 200 {
		t.Fatalf("tokens=%d", res.ReturnedTokens)
	}
	if len(res.Selected) == 0 {
		t.Fatalf("none selected: %+v", res)
	}
	joined := strings.ToLower(res.Markdown)
	if !strings.Contains(joined, "pricing") && !strings.Contains(joined, "enterprise") {
		t.Fatalf("unexpected selection: %s", res.Markdown)
	}
}

func TestPackSections(t *testing.T) {
	ctr, _ := tokens.New("cl100k_base")
	doc := document.BuildFromMarkdown("# A\n\nfoo\n\n## B\n\nbar\n", document.Document{URL: "u"}, ctr)
	res := PackSections(doc, []string{"b", "missing"}, 50, ctr)
	if len(res.Selected) != 1 || res.Selected[0] != "b" {
		t.Fatalf("%+v", res)
	}
}
