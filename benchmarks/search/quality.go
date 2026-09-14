//go:build benchmark

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gibbok/chiedi/internal/embedding"
	"github.com/gibbok/chiedi/internal/retrieval"
	"github.com/gibbok/chiedi/internal/store"
)

const qualityLimit = 40

type qualityQuery struct {
	ID       string   `json:"id"`
	Category string   `json:"category"`
	Text     string   `json:"text"`
	Relevant []string `json:"relevant_documents"`
}

type qualityDocument struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type qualityFixture struct {
	Version   string            `json:"version"`
	Documents []qualityDocument `json:"documents"`
	Queries   []qualityQuery    `json:"queries"`
}

type qualityMetrics struct {
	Recall1  float64 `json:"recall_at_1"`
	Recall5  float64 `json:"recall_at_5"`
	Recall10 float64 `json:"recall_at_10"`
	MRR      float64 `json:"mrr"`
}

type qualityResult struct {
	QueryID   string   `json:"query_id"`
	Category  string   `json:"category"`
	Mode      string   `json:"mode"`
	Documents []string `json:"ranked_documents"`
	qualityMetrics
}

func recordID(i int) string { return fmt.Sprintf("record-%05d", i) }
func recordPath(i int) string {
	ext := ".txt"
	if i%2 == 1 {
		ext = ".pdf"
	}
	return recordID(i) + ext
}

// Judgments describe the corpus topics, independently of search results or model output.
func qualityQueries(n int) []qualityQuery {
	var out []qualityQuery
	// Select one pair per topic, spread across its available archive positions.
	// A uniform stride can alias the ten-topic cycle (e.g. at 182 documents).
	for topic := 0; topic < len(topics); topic++ {
		pairs := (n/2-1-topic)/len(topics) + 1
		pair := topic + len(topics)*(topic*(pairs-1)/(len(topics)-1))
		for offset := 0; offset < 2; offset++ {
			i := 2*pair + offset
			out = append(out, qualityQuery{fmt.Sprintf("exact-%02d", 2*topic+offset), "exact-reference", fmt.Sprintf("REF%05dS0", i), []string{recordID(i)}})
		}
	}
	paraphrases := []string{
		"thermal protection roofing contractor", "travel accommodation booking",
		"automobile garage fix", "kid classroom fees", "physician laboratory visit",
		"codebase storage migration", "utility water consumption", "worker wage agreement",
		"planting irrigation tools", "furniture appliance cost",
	}
	italian := []string{"isolamento del tetto", "prenotazione albergo per le vacanze", "riparazione automobile", "pagamento della scuola", "visita medica", "migrazione del database", "bolletta del riscaldamento", "ferie e stipendio", "attrezzatura da giardino", "prezzo degli elettrodomestici"}
	czech := []string{"izolace střechy", "rezervace hotelu na dovolenou", "oprava automobilu", "platba školného", "lékařské vyšetření", "migrace databáze", "účet za vytápění", "dovolená a mzda", "zahradní vybavení", "cena domácích spotřebičů"}
	for topic, q := range queries {
		var relevant []string
		for i := 0; i < n; i++ {
			if (i/2)%len(topics) == topic {
				relevant = append(relevant, recordID(i))
			}
		}
		out = append(out, qualityQuery{fmt.Sprintf("topic-%02d", topic), "topic", q, relevant})
		out = append(out, qualityQuery{fmt.Sprintf("paraphrase-%02d", topic), "paraphrase", paraphrases[topic], relevant})
		out = append(out, qualityQuery{fmt.Sprintf("italian-%02d", topic), "italian-to-english", italian[topic], relevant})
		out = append(out, qualityQuery{fmt.Sprintf("czech-%02d", topic), "czech-to-english", czech[topic], relevant})
	}
	return out
}

// Collapse duplicate chunks BEFORE applying document cutoffs, retaining first occurrence.
func uniqueDocuments(ranked []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, id := range ranked {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func scoreQuality(ranked, relevant []string) qualityMetrics {
	rel := map[string]bool{}
	for _, id := range relevant {
		rel[id] = true
	}
	var m qualityMetrics
	if len(rel) == 0 {
		return m
	} // Fixture validation rejects this for benchmark queries.
	var hits1, hits5, hits10 int
	for i, id := range uniqueDocuments(ranked) {
		if !rel[id] {
			continue
		}
		if i < 1 {
			hits1++
		}
		if i < 5 {
			hits5++
		}
		if i < 10 {
			hits10++
		}
		if m.MRR == 0 {
			m.MRR = 1 / float64(i+1)
		}
	}
	m.Recall1 = float64(hits1) / float64(len(rel))
	m.Recall5 = float64(hits5) / float64(len(rel))
	m.Recall10 = float64(hits10) / float64(len(rel))
	return m
}

// Candidates is ordered by chunk ID, not relevance. Preserve the ranks supplied by SQL.
func rankedCandidates(c []store.Candidate, mode string) ([]string, error) {
	rank := func(c store.Candidate) int {
		if mode == "vector" {
			return c.VectorRank
		}
		return c.LexicalRank
	}
	for _, v := range c {
		if rank(v) < 1 || (mode == "vector" && v.LexicalRank != 0) || (mode == "full-text" && v.VectorRank != 0) {
			return nil, fmt.Errorf("quality mode isolation failed: %s", mode)
		}
	}
	sort.Slice(c, func(i, j int) bool { return rank(c[i]) < rank(c[j]) })
	paths := make([]string, 0, len(c))
	for _, v := range c {
		paths = append(paths, v.Chunk.Path)
	}
	return paths, nil
}

func qualitySearch(ctx context.Context, s *store.Store, q, mode string) ([]string, error) {
	e := embedding.E5{}
	if mode == "hybrid" {
		hits, err := (retrieval.Retriever{Store: s, Embedder: e}).Retrieve(ctx, q, "", qualityLimit)
		if err != nil {
			return nil, err
		}
		paths := make([]string, 0, len(hits))
		for _, hit := range hits {
			paths = append(paths, hit.Path)
		}
		return paths, nil
	}
	vector := make([]float32, e.Dimensions())
	fts := `"` + strings.Join(strings.Fields(q), `" AND "`) + `"`
	if mode == "vector" {
		vectors, err := e.EmbedQuery(ctx, []string{q})
		if err != nil {
			return nil, err
		}
		vector, fts = vectors[0], ""
	}
	candidates, err := s.Candidates(ctx, vector, fts, "", qualityLimit)
	if err != nil {
		return nil, err
	}
	return rankedCandidates(candidates, mode)
}

func measureQuality(ctx context.Context, s *store.Store, folder string, n int) (string, error) {
	fixture := qualityFixture{Version: "corpus-v1/quality-v3", Queries: qualityQueries(n)}
	byPath := map[string]string{}
	for i := 0; i < n; i++ {
		path := recordPath(i)
		data, err := os.ReadFile(filepath.Join(folder, "documents", path))
		if err != nil {
			return "", err
		}
		fixture.Documents = append(fixture.Documents, qualityDocument{recordID(i), path, fmt.Sprintf("%x", sha256.Sum256(data))})
		byPath[path] = recordID(i)
	}
	if err := validateFixture(fixture); err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		return "", err
	}
	if err = os.WriteFile(filepath.Join(folder, "ground-truth.json"), raw, 0644); err != nil {
		return "", err
	}
	var results []qualityResult
	for _, q := range fixture.Queries {
		for _, mode := range []string{"full-text", "vector", "hybrid"} {
			paths, err := qualitySearch(ctx, s, q.Text, mode)
			if err != nil {
				return "", fmt.Errorf("%s/%s: %w", q.ID, mode, err)
			}
			ids := []string{}
			for _, path := range paths {
				id, ok := byPath[filepath.ToSlash(path)]
				if !ok {
					return "", fmt.Errorf("unknown result path %q", path)
				}
				ids = append(ids, id)
			}
			ids = uniqueDocuments(ids)
			results = append(results, qualityResult{q.ID, q.Category, mode, ids, scoreQuality(ids, q.Relevant)})
		}
	}
	rawResults, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return "", err
	}
	if err = os.WriteFile(filepath.Join(folder, "quality-results.json"), rawResults, 0644); err != nil {
		return "", err
	}
	return qualityMarkdown(results, fmt.Sprintf("%x", sha256.Sum256(raw))), nil
}

func validateFixture(f qualityFixture) error {
	ids, paths, queries := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, d := range f.Documents {
		if d.ID == "" || d.Path == "" || ids[d.ID] || paths[d.Path] {
			return fmt.Errorf("invalid or duplicate ground-truth document: %q", d.ID)
		}
		ids[d.ID], paths[d.Path] = true, true
	}
	if len(f.Queries) == 0 {
		return fmt.Errorf("ground truth has no queries")
	}
	for _, q := range f.Queries {
		if q.ID == "" || queries[q.ID] || q.Category == "" || strings.TrimSpace(q.Text) == "" || len(q.Relevant) == 0 {
			return fmt.Errorf("invalid ground-truth query: %q", q.ID)
		}
		queries[q.ID] = true
		seen := map[string]bool{}
		for _, id := range q.Relevant {
			if !ids[id] || seen[id] {
				return fmt.Errorf("invalid relevance judgment %q for %q", id, q.ID)
			}
			seen[id] = true
		}
	}
	return nil
}

func qualityMarkdown(results []qualityResult, hash string) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "\n#### Retrieval accuracy\n\nGround truth SHA-256: `%s`. Corpus v1 / quality v3.\n\nDocument-level macro averages; duplicate chunks retain their first rank. Recall@k = relevant documents in the first k unique results / all relevant documents. MRR = mean reciprocal rank of the first relevant document in the returned pool (zero on a miss). Each mode returns at most 40 chunks; hybrid still uses its production 40-candidate budget per retrieval path. MRR is bounded by that pool, not exhaustive corpus MRR. No refill after deduplication.\n\n| Mode | Category | Queries | Recall@1 | Recall@5 | Recall@10 | MRR (40-chunk pool) |\n|---|---|---:|---:|---:|---:|---:|\n", hash)
	for _, mode := range []string{"full-text", "vector", "hybrid"} {
		for _, category := range []string{"all", "exact-reference", "topic", "paraphrase", "italian-to-english", "czech-to-english"} {
			var m qualityMetrics
			count := 0
			for _, r := range results {
				if r.Mode != mode || (category != "all" && r.Category != category) {
					continue
				}
				count++
				m.Recall1 += r.Recall1
				m.Recall5 += r.Recall5
				m.Recall10 += r.Recall10
				m.MRR += r.MRR
			}
			if count > 0 {
				d := float64(count)
				fmt.Fprintf(&b, "| %s | %s | %d | %.4f | %.4f | %.4f | %.4f |\n", mode, category, count, m.Recall1/d, m.Recall5/d, m.Recall10/d, m.MRR/d)
			}
		}
	}
	fmt.Fprintln(&b, "\nExact-reference queries have one relevant document; topic and paraphrase queries include every record in that topic. Topic Recall@k can be at most k / relevant-count, so it naturally falls as the corpus grows. These synthetic judgments do not establish real-world semantic quality. Empty results score zero; retrieval errors fail the run. Rankings and per-query scores are in quality-results.json; queries, judgments and corpus hashes are in ground-truth.json.")
	return b.String()
}
