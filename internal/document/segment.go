package document

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/asmseo/mdcix/internal/tokens"
)

var (
	headingRe = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)
	fenceRe   = regexp.MustCompile("(?m)^```")
)

// Segment splits markdown into hierarchical sections.
// Uses heading hierarchy when present; otherwise paragraph/block heuristics.
func Segment(md string, ctr *tokens.Counter) []Section {
	if ctr == nil {
		ctr = &tokens.Counter{}
	}
	md = strings.TrimSpace(md)
	if md == "" {
		return nil
	}
	lines := strings.Split(md, "\n")
	var secs []Section
	if hasPublisherHeadings(lines) {
		secs = segmentByHeadings(lines, ctr)
	} else {
		secs = segmentUnstructured(lines, ctr)
	}
	sort.SliceStable(secs, func(i, j int) bool { return secs[i].Position < secs[j].Position })
	return secs
}

func hasPublisherHeadings(lines []string) bool {
	inFence := false
	for _, line := range lines {
		if fenceRe.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if headingRe.MatchString(line) {
			return true
		}
	}
	return false
}

func segmentByHeadings(lines []string, ctr *tokens.Counter) []Section {
	type frame struct {
		sec    Section
		buf    []string
		parent string
	}
	var stack []frame
	var out []Section
	counts := map[string]int{}
	inFence := false
	pos := 0

	flush := func(f *frame) {
		body := strings.TrimSpace(strings.Join(f.buf, "\n"))
		// Keep heading line in section markdown for fidelity
		head := strings.Repeat("#", f.sec.Level) + " " + f.sec.Title
		md := head
		if body != "" {
			md = head + "\n\n" + body
		}
		f.sec.Markdown = md
		f.sec.Tokens = ctr.Count(md)
		f.sec.Links = ExtractLinks(md)
		if oversizedAtomic(md) {
			f.sec.Warnings = append(f.sec.Warnings, "oversized_atomic_block")
		}
		out = append(out, f.sec)
	}

	ensureRoot := func() {
		if len(stack) > 0 {
			return
		}
		id := nextID("preamble", counts)
		stack = append(stack, frame{
			sec: Section{ID: id, Title: "(preamble)", Level: 1, Position: pos, Inferred: true},
		})
		pos++
	}

	for _, line := range lines {
		if fenceRe.MatchString(line) {
			inFence = !inFence
			ensureRoot()
			stack[len(stack)-1].buf = append(stack[len(stack)-1].buf, line)
			continue
		}
		if !inFence {
			if m := headingRe.FindStringSubmatch(line); m != nil {
				level := len(m[1])
				title := strings.TrimSpace(m[2])
				// pop deeper/equal
				for len(stack) > 0 && stack[len(stack)-1].sec.Level >= level {
					f := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					flush(&f)
				}
				parent := ""
				if len(stack) > 0 {
					parent = stack[len(stack)-1].sec.ID
				}
				id := nextID(title, counts)
				stack = append(stack, frame{
					sec: Section{
						ID: id, Title: title, Level: level, ParentID: parent, Position: pos,
					},
				})
				pos++
				continue
			}
		}
		ensureRoot()
		stack[len(stack)-1].buf = append(stack[len(stack)-1].buf, line)
	}
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		flush(&f)
	}
	// Drop empty preamble-only if nothing useful
	if len(out) == 1 && out[0].Inferred && strings.TrimSpace(strings.TrimPrefix(out[0].Markdown, "# (preamble)")) == "" {
		return nil
	}
	return out
}

func segmentUnstructured(lines []string, ctr *tokens.Counter) []Section {
	var blocks [][]string
	var cur []string
	flushBlock := func() {
		if len(cur) == 0 {
			return
		}
		blocks = append(blocks, cur)
		cur = nil
	}
	inFence := false
	for _, line := range lines {
		if fenceRe.MatchString(line) {
			inFence = !inFence
			cur = append(cur, line)
			continue
		}
		if !inFence && strings.TrimSpace(line) == "" {
			flushBlock()
			continue
		}
		cur = append(cur, line)
	}
	flushBlock()

	counts := map[string]int{}
	var secs []Section
	pos := 0
	const softMax = 800 // token soft target per chunk
	var chunk []string
	chunkTokens := 0
	emit := func(title string, body []string, inferred bool) {
		md := strings.TrimSpace(strings.Join(body, "\n"))
		if md == "" {
			return
		}
		if title == "" {
			title = firstLineTitle(md)
		}
		id := nextID(title, counts)
		s := Section{
			ID: id, Title: title, Level: 2, Position: pos,
			Markdown: md, Tokens: ctr.Count(md), Inferred: inferred,
			Links: ExtractLinks(md),
		}
		if oversizedAtomic(md) {
			s.Warnings = append(s.Warnings, "oversized_atomic_block")
		}
		secs = append(secs, s)
		pos++
	}

	for _, b := range blocks {
		text := strings.Join(b, "\n")
		t := ctr.Count(text)
		// short bold / visually prominent line starts a new inferred section
		if isLikelyHeading(b) {
			if len(chunk) > 0 {
				emit("", chunk, true)
				chunk, chunkTokens = nil, 0
			}
			title := firstLineTitle(strings.Join(b, "\n"))
			// keep heading line inside the next chunk so body attaches to it
			chunk = append(chunk, b...)
			chunk = append(chunk, "")
			chunkTokens = t
			_ = title
			continue
		}
		if chunkTokens+t > softMax && len(chunk) > 0 {
			emit("", chunk, true)
			chunk, chunkTokens = nil, 0
		}
		chunk = append(chunk, b...)
		chunk = append(chunk, "")
		chunkTokens += t
	}
	if len(chunk) > 0 {
		emit("", chunk, true)
	}
	return secs
}

func nextID(title string, counts map[string]int) string {
	base := slugify(title)
	if base == "" {
		base = "section"
	}
	counts[base]++
	n := counts[base]
	if n <= 1 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, n)
}

func isLikelyHeading(block []string) bool {
	if len(block) != 1 {
		return false
	}
	line := strings.TrimSpace(block[0])
	if len(line) == 0 || len(line) > 80 {
		return false
	}
	if strings.HasPrefix(line, "**") && strings.HasSuffix(line, "**") {
		return true
	}
	words := strings.Fields(line)
	return len(words) > 0 && len(words) <= 10 && !strings.HasSuffix(line, ".")
}

func firstLineTitle(md string) string {
	for _, line := range strings.Split(md, "\n") {
		line = strings.TrimSpace(stripMDNoise(line))
		if line != "" {
			return truncate(line, 60)
		}
	}
	return "(untitled)"
}

func oversizedAtomic(md string) bool {
	// crude: fenced block or table larger than ~2k chars marked
	if strings.Count(md, "```") >= 2 && len(md) > 4000 {
		return true
	}
	if strings.Count(md, "|") > 20 && len(md) > 4000 {
		return true
	}
	return false
}

var linkRe = regexp.MustCompile(`\[([^\]]*)\]\(([^)]+)\)`)

// ExtractLinks returns markdown links.
func ExtractLinks(md string) []Link {
	ms := linkRe.FindAllStringSubmatch(md, -1)
	var out []Link
	seen := map[string]struct{}{}
	for _, m := range ms {
		href := strings.TrimSpace(m[2])
		if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "data:") {
			continue
		}
		key := href + "|" + m[1]
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, Link{Text: strings.TrimSpace(m[1]), Href: href})
	}
	return out
}
