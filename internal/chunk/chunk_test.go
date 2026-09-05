package chunk

import (
	"strings"
	"testing"

	"github.com/gibbok/local-genius/internal/extract"
)

func TestDeterministicBoundedChunks(t *testing.T) {
	doc := extract.Document{Title: "Guide", Sections: []extract.Section{{Heading: "Rules", Text: strings.Repeat("é", MaxRunes+100)}}}
	a, b := Document(doc), Document(doc)
	if len(a) != 2 || len(b) != 2 {
		t.Fatalf("chunk counts %d %d", len(a), len(b))
	}
	for i := range a {
		if a[i].EmbeddingHash != b[i].EmbeddingHash || len([]rune(a[i].Text)) > MaxRunes {
			t.Fatalf("chunk %d is not deterministic/bounded", i)
		}
	}
	if got:=len([]rune(a[1].Text));got!=100+OverlapRunes{t.Fatalf("hard-split overlap=%d runes, want %d",got-100,OverlapRunes)}
}
