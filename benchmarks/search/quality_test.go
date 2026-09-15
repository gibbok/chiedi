//go:build benchmark

package main

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/gibbok/chiedi/internal/store"
)

func TestQualityMetrics(t *testing.T) {
	tests := []struct {
		name             string
		ranked, relevant []string
		want             qualityMetrics
	}{
		{"first", []string{"a"}, []string{"a"}, qualityMetrics{1, 1, 1, 1}},
		{"duplicates", []string{"x", "x", "a", "a", "b"}, []string{"a", "b"}, qualityMetrics{0, 1, 1, .5}},
		{"partial recall", []string{"a"}, []string{"a", "b"}, qualityMetrics{.5, .5, .5, 1}},
		{"empty", nil, []string{"a"}, qualityMetrics{}},
		{"miss", []string{"x"}, []string{"a"}, qualityMetrics{}},
		{"rank five", []string{"v", "w", "x", "y", "a"}, []string{"a"}, qualityMetrics{0, 1, 1, .2}},
		{"rank ten", []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "a"}, []string{"a"}, qualityMetrics{0, 0, 1, .1}},
		{"rank eleven", []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "a"}, []string{"a"}, qualityMetrics{0, 0, 0, 1.0 / 11}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scoreQuality(tt.ranked, tt.relevant); got != tt.want {
				t.Fatalf("got %+v want %+v", got, tt.want)
			}
		})
	}
}

func TestCandidateRanking(t *testing.T) {
	for _, mode := range []string{"vector", "full-text"} {
		c := []store.Candidate{{Chunk: store.Chunk{ID: 1, Path: "second"}}, {Chunk: store.Chunk{ID: 2, Path: "first"}}}
		if mode == "vector" {
			c[0].VectorRank = 2
			c[1].VectorRank = 1
		} else {
			c[0].LexicalRank = 2
			c[1].LexicalRank = 1
		}
		got, err := rankedCandidates(c, mode)
		if err != nil || !reflect.DeepEqual(got, []string{"first", "second"}) {
			t.Fatalf("%s: %v %v", mode, got, err)
		}
		c[0].VectorRank = 1
		c[0].LexicalRank = 1
		if _, err := rankedCandidates(c, mode); err == nil {
			t.Fatal("mixed mode accepted")
		}
	}
}

func TestQualityFixture(t *testing.T) {
	for _, n := range []int{20, 21, 100, 182, 183, 362, 500, 2000, 10000} {
		f := qualityFixture{Queries: qualityQueries(n)}
		for i := 0; i < n; i++ {
			f.Documents = append(f.Documents, qualityDocument{ID: recordID(i), Path: recordPath(i)})
		}
		if err := validateFixture(f); err != nil {
			t.Fatal(n, err)
		}
		if len(f.Queries) != 60 || !reflect.DeepEqual(f.Queries, qualityQueries(n)) {
			t.Fatal("unstable query suite")
		}
		seen := map[string]bool{}
		coverage := map[int]map[int]bool{}
		for _, q := range f.Queries[:20] {
			id := q.Relevant[0]
			if seen[id] {
				t.Fatal("duplicate exact target")
			}
			seen[id] = true
			i, err := strconv.Atoi(strings.TrimPrefix(id, "record-"))
			if err != nil {
				t.Fatal(err)
			}
			topic := (i / 2) % len(topics)
			if coverage[topic] == nil {
				coverage[topic] = map[int]bool{}
			}
			coverage[topic][i%2] = true
			if !strings.Contains(document(i), q.Text) {
				t.Fatal("reference absent from target document", id)
			}
		}
		for topic := range topics {
			if len(coverage[topic]) != 2 {
				t.Fatalf("size %d: topic %d missing a format in exact-reference sample", n, topic)
			}
		}
	}
	f := qualityFixture{Documents: []qualityDocument{{ID: "a", Path: "a.txt"}}, Queries: []qualityQuery{{ID: "q", Category: "topic", Text: "x", Relevant: []string{"missing"}}}}
	if validateFixture(f) == nil {
		t.Fatal("unknown ground truth accepted")
	}
	f.Queries[0].Relevant = nil
	if validateFixture(f) == nil {
		t.Fatal("empty ground truth accepted")
	}
	f.Queries[0].Relevant = []string{"a", "a"}
	if validateFixture(f) == nil {
		t.Fatal("duplicate judgment accepted")
	}
}

func TestQualityMacroAverage(t *testing.T) {
	rows := []qualityResult{
		{Category: "topic", Mode: "vector", qualityMetrics: qualityMetrics{1, 1, 1, 1}},
		{Category: "topic", Mode: "vector"},
		{Category: "exact-reference", Mode: "vector", qualityMetrics: qualityMetrics{1, 1, 1, 1}},
	}
	md := qualityMarkdown(rows, "hash")
	if !strings.Contains(md, "| vector | topic | 2 | 0.5000 | 0.5000 | 0.5000 | 0.5000 |") || !strings.Contains(md, "| vector | all | 3 | 0.6667") {
		t.Fatal(md)
	}
}

// Rebuild in separate directories to catch dependence on absolute paths, IDs or map order.
func TestQualityReproducible(t *testing.T) {
	var previous [][]byte
	for run := 0; run < 2; run++ {
		dir := t.TempDir()
		if _, _, err := scenario(context.Background(), dir, 20, 1); err != nil {
			t.Fatal(err)
		}
		var current [][]byte
		for _, name := range []string{"ground-truth.json", "quality-results.json"} {
			data, err := os.ReadFile(filepath.Join(dir, "20", name))
			if err != nil {
				t.Fatal(err)
			}
			current = append(current, data)
		}
		if run > 0 && !reflect.DeepEqual(previous, current) {
			t.Fatal("quality artifacts differ between fresh indexes")
		}
		previous = current
		var results []qualityResult
		if err := json.Unmarshal(current[1], &results); err != nil {
			t.Fatal(err)
		}
		if len(results) != 180 {
			t.Fatalf("got %d rows", len(results))
		}
		for _, r := range results {
			if len(r.Documents) != len(uniqueDocuments(r.Documents)) {
				t.Fatal("duplicate result documents")
			}
			for _, v := range []float64{r.Recall1, r.Recall5, r.Recall10, r.MRR} {
				if math.IsNaN(v) || v < 0 || v > 1.000000001 {
					t.Fatal("invalid metric", r)
				}
			}
			if r.Recall1 > r.Recall5 || r.Recall5 > r.Recall10 {
				t.Fatal("non-monotonic recall")
			}
		}
	}
}
