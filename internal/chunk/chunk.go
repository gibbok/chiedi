package chunk

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/gibbok/local-genius/internal/extract"
)

const (
	TargetRunes = 1800
	MaxRunes    = 3000
	OverlapRunes = 200
)

type Chunk struct {
	Ordinal        int
	Heading        string
	PageStart      int
	PageEnd        int
	Text           string
	EmbeddingText  string
	EmbeddingHash  [32]byte
}

func Document(doc extract.Document) []Chunk {
	var result []Chunk
	for _, section := range doc.Sections {
		paragraphs := splitParagraphs(section.Text)
		var current string
		flush := func() {
			current = strings.TrimSpace(current)
			if current == "" {
				return
			}
			embedText := embeddingText(doc.Title, section.Heading, current)
			result = append(result, Chunk{
				Ordinal: len(result), Heading: section.Heading,
				PageStart: section.PageStart, PageEnd: section.PageEnd,
				Text: current, EmbeddingText: embedText, EmbeddingHash: sha256.Sum256([]byte(embedText)),
			})
			current = ""
		}
		for _, paragraph := range paragraphs {
			for _, piece := range hardSplit(paragraph, MaxRunes) {
				separator := ""
				if current != "" {
					separator = "\n\n"
				}
				if utf8.RuneCountInString(current+separator+piece) > TargetRunes && current != "" {
					flush()
					separator = ""
				}
				current += separator + piece
			}
		}
		flush()
	}
	return result
}

func embeddingText(title, heading, text string) string {
	return fmt.Sprintf("Document: %s\nSection: %s\n\n%s", strings.TrimSpace(title), strings.TrimSpace(heading), strings.TrimSpace(text))
}

func splitParagraphs(s string) []string {
	parts := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n\n")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func hardSplit(s string, max int) []string {
	runes := []rune(s)
	if len(runes) <= max {
		return []string{s}
	}
	if max <= 0 {
		return nil
	}
	overlap:=OverlapRunes;if overlap>=max{overlap=0}
	var out []string
	for start:=0;start<len(runes);{
		end:=start+max;if end>len(runes){end=len(runes)}
		out=append(out,string(runes[start:end]))
		if end==len(runes){break}
		start=end-overlap
	}
	return out
}
