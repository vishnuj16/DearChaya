// Package story holds the bundled story text and turns its light markdown
// (a "# Title" line, blank-line paragraphs, *italics*) into renderable blocks.
package story

import (
	_ "embed"
	"math"
	"strings"
)

//go:embed story.md
var source string

// Default returns the bundled story, split into title and body, for seeding the database.
func Default() (title, body string) {
	return splitTitle(source)
}

func splitTitle(md string) (title, body string) {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	first, rest, _ := strings.Cut(md, "\n")
	if h, ok := strings.CutPrefix(strings.TrimSpace(first), "# "); ok {
		return strings.TrimSuffix(strings.TrimSpace(h), ","), strings.TrimSpace(rest)
	}
	return "Dear Chaya", strings.TrimSpace(md)
}

// Span is a run of text that is either italic or not.
type Span struct {
	Text   string
	Italic bool
}

// Paragraph is a sequence of spans. Newlines inside spans are hard line breaks.
// Callout marks a "> " blockquote, used for Kiran's louder thoughts.
type Paragraph struct {
	Spans   []Span
	Callout bool
}

// View is everything the page template needs to render the story.
type View struct {
	Title          string
	Raw            string // the markdown body, for the author's editor
	Paragraphs     []Paragraph
	ReadingMinutes int
}

const wordsPerMinute = 230

// NewView parses the story body into paragraphs and estimates reading time.
func NewView(title, body string) View {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	var paras []Paragraph
	for _, block := range strings.Split(body, "\n\n") {
		lines := strings.Split(strings.TrimSpace(block), "\n")
		callout := true
		for i, l := range lines {
			l = strings.TrimSpace(l)
			if q, ok := strings.CutPrefix(l, ">"); ok {
				l = strings.TrimSpace(q)
			} else {
				callout = false
			}
			lines[i] = l
		}
		text := strings.Join(lines, "\n")
		if text == "" {
			continue
		}
		paras = append(paras, Paragraph{Spans: parseSpans(text), Callout: callout})
	}
	minutes := int(math.Ceil(float64(len(strings.Fields(body))) / wordsPerMinute))
	return View{Title: title, Raw: body, Paragraphs: paras, ReadingMinutes: max(minutes, 1)}
}

// parseSpans toggles italics on '*' and honours markdown backslash escapes
// (e.g. "40\."). If an italic run is never closed, the '*' stays literal.
func parseSpans(text string) []Span {
	var (
		spans  []Span
		cur    strings.Builder
		italic bool
	)
	flush := func() {
		if cur.Len() > 0 {
			spans = append(spans, Span{Text: cur.String(), Italic: italic})
			cur.Reset()
		}
	}
	for i := 0; i < len(text); i++ {
		switch c := text[i]; {
		case c == '\\' && i+1 < len(text) && strings.IndexByte(`\*_.!#>-[]()`, text[i+1]) >= 0:
			i++
			cur.WriteByte(text[i])
		case c == '*':
			flush()
			italic = !italic
		default:
			cur.WriteByte(c)
		}
	}
	if italic {
		// Unbalanced marker: render the original text as-is (minus escapes).
		return []Span{{Text: strings.ReplaceAll(text, "\\", "")}}
	}
	flush()
	return spans
}
