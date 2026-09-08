//go:build benchmark

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gibbok/chiedi/internal/extract"
)

func TestCorpusPDFExtraction(t *testing.T) {
	for _, i := range []int{1, 9, 19} {
		path := filepath.Join(t.TempDir(), "record.pdf")
		if err := os.WriteFile(path, pdf(document(i)), 0644); err != nil {
			t.Fatal(err)
		}
		doc, err := extract.File(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		var text strings.Builder
		for _, section := range doc.Sections {
			text.WriteString(section.Text)
		}
		for _, want := range []string{topics[(i/2)%len(topics)], fmt.Sprintf("REF%05dS%d", i, 2+i%5)} {
			if !strings.Contains(text.String(), want) {
				t.Fatalf("PDF %d missing %q: %s", i, want, text.String())
			}
		}
	}
}

func TestSizes(t *testing.T) {
	for _, raw := range []string{"", "0", "19", "10001", "100,100", "abc"} {
		if _, err := parseSizes(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	if n, err := parseSizes("100, 500,2000"); err != nil || len(n) != 3 {
		t.Fatalf("%v %v", n, err)
	}
}

func TestScenario(t *testing.T) {
	summary, rows, err := scenario(context.Background(), t.TempDir(), 20, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary, "| 20 | 10 |") || strings.Count(rows, "| vector |") != len(queries) {
		t.Fatalf("incomplete report: %s\n%s", summary, rows)
	}
}

func TestPercentiles(t *testing.T) {
	a := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if percentile(a, .5) != 5 || percentile(a, .95) != 10 {
		t.Fatal("incorrect nearest-rank percentile")
	}
}
