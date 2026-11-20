// Package document defines the structured, sectioned representation of a web page.
package document

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/asmseo/mdcix/internal/tokens"
)

// Document is a versioned, addressable snapshot of extracted web content.
type Document struct {
	URL              string    `json:"url"`
	FinalURL         string    `json:"final_url"`
	CanonicalURL     string    `json:"canonical_url,omitempty"`
	Title            string    `json:"title,omitempty"`
	Language         string    `json:"language,omitempty"`
	RetrievedAt      time.Time `json:"retrieved_at"`
	HTTPStatus       int       `json:"http_status"`
	ExtractionMethod string    `json:"extraction_method"` // native | converted | unstructured
	ContentHash      string    `json:"content_hash"`
	Version          string    `json:"document_version"`
	OriginalTokens   int       `json:"original_tokens"`
	MarkdownTokens   int       `json:"markdown_tokens"`
	Markdown         string    `json:"markdown,omitempty"`
	Attribution      string    `json:"publisher_attribution,omitempty"`
	Warnings         []string  `json:"warnings,omitempty"`
	Links            []Link    `json:"links,omitempty"`
	Sections         []Section `json:"sections"`
}

// Section is a hierarchical, token-counted unit of the document.
type Section struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Level        int      `json:"level"`
	ParentID     string   `json:"parent_id,omitempty"`
	Position     int      `json:"position"`
	Markdown     string   `json:"markdown"`
	Tokens       int      `json:"tokens"`
	SourceAnchor string   `json:"source_anchor,omitempty"`
	Inferred     bool     `json:"inferred,omitempty"` // heading inferred, not publisher Hn
	Links        []Link   `json:"links,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
}

// Link is a hyperlink found in content.
type Link struct {
	Text string `json:"text,omitempty"`
	Href string `json:"href"`
}

// Outline is a compact representation for agent-selected retrieval.
type Outline struct {
	URL             string           `json:"url"`
	DocumentVersion string           `json:"document_version"`
	Title           string           `json:"title,omitempty"`
	TotalTokens     int              `json:"total_tokens"`
	Warnings        []string         `json:"warnings,omitempty"`
	Sections        []OutlineSection `json:"sections"`
}

// OutlineSection is a compact section row.
type OutlineSection struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Tokens  int    `json:"tokens"`
	Level   int    `json:"level"`
	Preview string `json:"preview,omitempty"`
}

// BuildFromMarkdown segments markdown into a Document.
func BuildFromMarkdown(md string, meta Document, ctr *tokens.Counter) Document {
	if ctr == nil {
		ctr, _ = tokens.Default()
		if ctr == nil {
			ctr = &tokens.Counter{}
		}
	}
	meta.Markdown = strings.TrimSpace(md)
	meta.RetrievedAt = time.Now().UTC()
	if meta.FinalURL == "" {
		meta.FinalURL = meta.URL
	}
	meta.ContentHash = HashContent(meta.Markdown)
	meta.Version = meta.ContentHash[:12]
	meta.MarkdownTokens = ctr.Count(meta.Markdown)
	meta.Links = ExtractLinks(meta.Markdown)

	secs := Segment(meta.Markdown, ctr)
	if len(secs) == 0 && meta.Markdown != "" {
		secs = []Section{fallbackSection(meta.Markdown, ctr)}
		meta.Warnings = append(meta.Warnings, "no_headings: used single-section fallback")
		if meta.ExtractionMethod == "" {
			meta.ExtractionMethod = "unstructured"
		}
	} else if meta.ExtractionMethod == "" {
		meta.ExtractionMethod = "converted"
	}
	meta.Sections = secs
	if meta.Title == "" {
		meta.Title = inferTitle(secs, meta.Markdown)
	}
	return meta
}

// ToOutline builds a compact outline (does not omit sections).
func (d Document) ToOutline(previewRunes int) Outline {
	if previewRunes <= 0 {
		previewRunes = 80
	}
	out := Outline{
		URL:             d.URL,
		DocumentVersion: d.Version,
		Title:           d.Title,
		TotalTokens:     d.MarkdownTokens,
		Warnings:        d.Warnings,
	}
	for _, s := range d.Sections {
		prev := preview(s.Markdown, previewRunes)
		out.Sections = append(out.Sections, OutlineSection{
			ID: s.ID, Title: s.Title, Tokens: s.Tokens, Level: s.Level, Preview: prev,
		})
	}
	return out
}

// SectionByID returns a section or false.
func (d Document) SectionByID(id string) (Section, bool) {
	for _, s := range d.Sections {
		if s.ID == id {
			return s, true
		}
	}
	return Section{}, false
}

// SectionsByIDs returns sections in document order.
func (d Document) SectionsByIDs(ids []string) ([]Section, []string) {
	want := map[string]struct{}{}
	for _, id := range ids {
		want[id] = struct{}{}
	}
	var found []Section
	seen := map[string]struct{}{}
	for _, s := range d.Sections {
		if _, ok := want[s.ID]; ok {
			found = append(found, s)
			seen[s.ID] = struct{}{}
		}
	}
	var missing []string
	for _, id := range ids {
		if _, ok := seen[id]; !ok {
			missing = append(missing, id)
		}
	}
	return found, missing
}

// HashContent returns sha256 hex of content.
func HashContent(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// SlugID builds a deterministic section id from title + disambiguator.
func SlugID(title string, n int) string {
	base := slugify(title)
	if base == "" {
		base = "section"
	}
	if n <= 1 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, n)
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash && b.Len() > 0 {
			b.WriteByte('-')
			prevDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

func inferTitle(secs []Section, md string) string {
	for _, s := range secs {
		if s.Level == 1 && s.Title != "" {
			return s.Title
		}
	}
	for _, s := range secs {
		if s.Title != "" && s.Title != "(untitled)" {
			return s.Title
		}
	}
	line := strings.TrimSpace(strings.Split(md, "\n")[0])
	return truncate(line, 80)
}

func preview(md string, n int) string {
	plain := strings.Join(strings.Fields(stripMDNoise(md)), " ")
	return truncate(plain, n)
}

func stripMDNoise(s string) string {
	s = strings.ReplaceAll(s, "#", "")
	s = strings.ReplaceAll(s, "*", "")
	s = strings.ReplaceAll(s, "`", "")
	return s
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func fallbackSection(md string, ctr *tokens.Counter) Section {
	return Section{
		ID: "body", Title: "(untitled)", Level: 1, Position: 0,
		Markdown: md, Tokens: ctr.Count(md), Inferred: true,
	}
}
