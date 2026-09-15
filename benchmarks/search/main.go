//go:build benchmark

// Command search benchmarks production indexing and retrieval only when opted in.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gibbok/chiedi/internal/embedding"
	"github.com/gibbok/chiedi/internal/indexer"
	"github.com/gibbok/chiedi/internal/retrieval"
	"github.com/gibbok/chiedi/internal/store"
)

var topics = []string{
	"home renovation roof insulation contractor invoice warranty",
	"family holiday travel hotel reservation passport itinerary",
	"car insurance vehicle repair garage maintenance receipt",
	"child school tuition classroom calendar lunch payment",
	"doctor appointment medical examination laboratory prescription",
	"project database migration repository release deployment backup",
	"house electricity heating water meter utility bill",
	"employment contract salary annual leave agreement",
	"garden irrigation planting equipment purchase delivery",
	"household budget savings furniture appliance price",
}
var queries = []string{"roof insulation", "holiday reservation", "vehicle repair", "school payment", "medical examination", "database migration", "heating bill", "annual leave", "garden equipment", "appliance price"}

func main() {
	sizes := flag.String("sizes", "100,500,2000", "comma-separated document counts (20..10000)")
	repeats := flag.Int("repeats", 30, "measured repetitions per query and mode")
	flag.Parse()
	counts, err := parseSizes(*sizes)
	if err == nil && (flag.NArg() != 0 || *repeats < 1 || *repeats > 1000) {
		err = fmt.Errorf("require no positional arguments and repeats between 1 and 1000")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err = run(ctx, counts, *repeats); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseSizes(raw string) ([]int, error) {
	var out []int
	seen := map[int]bool{}
	for _, part := range strings.Split(raw, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 20 || n > 10000 || seen[n] {
			return nil, fmt.Errorf("sizes must be unique integers between 20 and 10000")
		}
		seen[n] = true
		out = append(out, n)
	}
	return out, nil
}

func run(ctx context.Context, sizes []int, repeats int) error {
	if _, err := os.Stat("go.mod"); err != nil {
		return fmt.Errorf("run from the repository root: %w", err)
	}
	base := "benchmarks/output"
	if err := os.MkdirAll(base, 0755); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(base, time.Now().UTC().Format("20060102T150405Z")+"-")
	if err != nil {
		return err
	}
	coldStart := time.Now()
	if _, err := (embedding.E5{}).EmbedQuery(ctx, []string{"Find the invoice"}); err != nil {
		return err
	}
	coldSeconds := time.Since(coldStart).Seconds()
	report := new(bytes.Buffer)
	fmt.Fprintln(report, "# Personal search benchmark\n\nStatus: COMPLETE (all indexing, integrity and query checks passed).")
	fmt.Fprintf(report, "\nUTC: %s\n\n## Environment\n\n```text\n%s\n```\n", time.Now().UTC().Format(time.RFC3339), hardware())
	fmt.Fprintf(report, "\n## Method\n\nCorpus v1: deterministic synthetic English family/project records, half text PDFs and half plain text (PDF count rounded down). Each document has 3–7 sections with unique record IDs, dates and amounts. No scans or OCR. Sizes: %v. Model: %s, %d dimensions.\n\nSingle client, persistent SQLite connection, sequential queries, no concurrent indexing. Each query/mode has 3 untimed warmups then %d samples; modes rotate each repetition. Percentiles use nearest rank. All samples are warm-cache, not cold disk measurements.\n\nVector-only uses Candidates with an empty FTS query; full-text-only uses Candidates with a zero vector (skips vector SQL). Both fetch source text and return up to 40 candidates. Query embedding is prepared outside these two timings. Hybrid uses the normal Retriever with limit 10 and includes query embedding and rank fusion. Timings exclude process startup, file reconciliation, indexing and output formatting. These are retrieval timings, not CLI end-to-end timings. Latency checks returned counts; a separate untimed pass measures document relevance against deterministic ground truth.\n", sizes, embedding.E5{}.ID(), embedding.E5{}.Dimensions(), repeats)
	fmt.Fprintf(report, "\nModel initialization plus first query: %.3f seconds (includes asset checksums). Subsequent query samples reuse the process-owned session.\n", coldSeconds)
	fmt.Fprintln(report, "\n## Results\n\n| Documents | PDFs | Chunks | Index seconds |\n|---:|---:|---:|---:|")
	var details bytes.Buffer
	for _, n := range sizes {
		fmt.Printf("Benchmarking %d documents...\n", n)
		summary, rows, err := scenario(ctx, dir, n, repeats)
		if err != nil {
			_ = os.WriteFile(filepath.Join(dir, "FAILED.txt"), []byte(err.Error()+"\n"), 0644)
			return fmt.Errorf("benchmark failed (artifacts in %s): %w", dir, err)
		}
		fmt.Fprintln(report, summary)
		fmt.Fprintln(&details, rows)
	}
	report.Write(details.Bytes())
	fmt.Fprintln(report, "\n## Interpretation\n\n100 documents represents a small active folder; 500 a household archive; 2,000 a larger personal archive or several projects. Synthetic short records are a reproducible baseline, not a prediction for long reports, scans, multilingual content, concurrent users, or different machine load. Rerun on the computer where chiedi will be used. Generated files, databases and this report are ignored by Git. Each run has a new directory; remove old output directories manually when no longer needed.")
	path := filepath.Join(dir, "results.md")
	if err := os.WriteFile(path, report.Bytes(), 0644); err != nil {
		return err
	}
	fmt.Println("Report:", path)
	return nil
}

func scenario(ctx context.Context, dir string, n, repeats int) (string, string, error) {
	folder := filepath.Join(dir, strconv.Itoa(n))
	corpus := filepath.Join(folder, "documents")
	if err := os.MkdirAll(corpus, 0755); err != nil {
		return "", "", err
	}
	for i := 0; i < n; i++ {
		if err := ctx.Err(); err != nil {
			return "", "", err
		}
		data := []byte(document(i))
		if i%2 == 1 {
			data = pdf(document(i))
		}
		if err := os.WriteFile(filepath.Join(corpus, recordPath(i)), data, 0644); err != nil {
			return "", "", err
		}
	}
	s, err := store.Open(ctx, filepath.Join(folder, "index.db"))
	if err != nil {
		return "", "", err
	}
	defer s.Close()
	if err = s.AddRoot(ctx, corpus); err != nil {
		return "", "", err
	}
	e := embedding.E5{}
	idx := indexer.Indexer{Store: s, Embedder: e}
	start := time.Now()
	stats, err := idx.Reconcile(ctx)
	elapsed := time.Since(start).Seconds()
	if err != nil {
		return "", "", err
	}
	if stats.Failed != 0 {
		return "", "", fmt.Errorf("failed documents: %v", stats.FailedDocumentPaths)
	}
	counts, err := s.Counts(ctx)
	if err != nil {
		return "", "", err
	}
	if counts.Indexed != n || counts.Documents != n || counts.Chunks < n {
		return "", "", fmt.Errorf("unexpected index counts: %+v", counts)
	}
	if err = s.IntegrityCheck(ctx); err != nil {
		return "", "", err
	}
	if err = s.IndexIntegrityCheck(ctx, e.Dimensions()); err != nil {
		return "", "", err
	}
	ret := retrieval.Retriever{Store: s, Embedder: e}
	var rows bytes.Buffer
	fmt.Fprintf(&rows, "\n### %d documents\n\n| Query | Mode | Samples | Min ms | Median ms | p95 ms | Max ms | Mean ms | Results min–max |\n|---|---|---:|---:|---:|---:|---:|---:|---:|\n", n)
	for _, q := range queries {
		v, err := e.EmbedQuery(ctx, []string{q})
		if err != nil {
			return "", "", err
		}
		fts := `"` + strings.Join(strings.Fields(q), `" AND "`) + `"`
		zero := make([]float32, e.Dimensions())
		// Assert that each database mode really excludes the other ranking path.
		for _, check := range []struct {
			vector     []float32
			query      string
			vectorOnly bool
		}{{zero, fts, false}, {v[0], "", true}} {
			candidates, err := s.Candidates(ctx, check.vector, check.query, "", 40)
			if err != nil {
				return "", "", err
			}
			for _, candidate := range candidates {
				if check.vectorOnly && (candidate.VectorRank < 1 || candidate.LexicalRank != 0) || !check.vectorOnly && (candidate.LexicalRank < 1 || candidate.VectorRank != 0) {
					return "", "", fmt.Errorf("search mode isolation failed for %q", q)
				}
			}
		}
		type mode struct {
			name string
			call func() (int, error)
		}
		modes := []mode{
			{"full-text", func() (int, error) { r, err := s.Candidates(ctx, zero, fts, "", 40); return len(r), err }},
			{"vector", func() (int, error) { r, err := s.Candidates(ctx, v[0], "", "", 40); return len(r), err }},
			{"hybrid", func() (int, error) { r, err := ret.Retrieve(ctx, q, "", 10); return len(r), err }},
		}
		samples := make([][]float64, len(modes))
		mins := []int{100, 100, 100}
		maxs := make([]int, len(modes))
		for rep := -3; rep < repeats; rep++ {
			for offset := range modes {
				m := (rep + 3 + offset) % len(modes)
				start := time.Now()
				hits, err := modes[m].call()
				ms := float64(time.Since(start)) / float64(time.Millisecond)
				if err != nil {
					return "", "", err
				}
				if hits < 1 {
					return "", "", fmt.Errorf("%s query %q returned no results", modes[m].name, q)
				}
				if rep >= 0 {
					samples[m] = append(samples[m], ms)
					mins[m] = min(mins[m], hits)
					maxs[m] = max(maxs[m], hits)
				}
			}
		}
		for m, mode := range modes {
			a := samples[m]
			sort.Float64s(a)
			sum := 0.0
			for _, x := range a {
				sum += x
			}
			fmt.Fprintf(&rows, "| %s | %s | %d | %.3f | %.3f | %.3f | %.3f | %.3f | %d–%d |\n", q, mode.name, len(a), a[0], percentile(a, .5), percentile(a, .95), a[len(a)-1], sum/float64(len(a)), mins[m], maxs[m])
		}
	}
	quality, err := measureQuality(ctx, s, folder, n)
	if err != nil {
		return "", "", err
	}
	rows.WriteString(quality)
	return fmt.Sprintf("| %d | %d | %d | %.3f |", n, n/2, counts.Chunks, elapsed), rows.String(), nil
}

func percentile(sorted []float64, p float64) float64 {
	return sorted[int(math.Ceil(float64(len(sorted))*p))-1]
}

func document(i int) string {
	var b strings.Builder
	topic := topics[(i/2)%len(topics)] // Every topic appears in both formats.
	for section := 0; section < 3+i%5; section++ {
		fmt.Fprintf(&b, "Record %05d section %d: %s.\n", i, section+1, topic)
		fmt.Fprintf(&b, "Date 2026-%02d-%02d. Reference REF%05dS%d. Amount %d EUR.\n", 1+i%12, 1+i%28, i, section, 50+(i*37+section*13)%4900)
		fmt.Fprint(&b, "The family keeps this record for planning and follow-up.\nReview the details with the project owner before the next visit.\n\n")
	}
	return b.String()
}

// Text PDF with explicit byte offsets and stream length, without extra dependencies.
func pdf(text string) []byte {
	var stream bytes.Buffer
	fmt.Fprintln(&stream, "BT /F1 9 Tf 40 800 Td 12 TL")
	for _, line := range strings.Split(text, "\n") {
		line = strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(line)
		fmt.Fprintf(&stream, "(%s) Tj T*\n", line)
	}
	fmt.Fprintln(&stream, "ET")
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 842 842] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>", fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", stream.Len(), stream.String())}
	var b bytes.Buffer
	fmt.Fprintln(&b, "%PDF-1.4")
	offsets := []int{0}
	for i, obj := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return b.Bytes()
}

func hardware() string {
	var b strings.Builder
	fmt.Fprintf(&b, "OS/architecture: %s/%s\nGo: %s\nLogical CPUs visible: %d\nGOMAXPROCS: %d\n", runtime.GOOS, runtime.GOARCH, runtime.Version(), runtime.NumCPU(), runtime.GOMAXPROCS(0))
	for _, path := range []string{"/proc/cpuinfo", "/proc/meminfo", "/sys/fs/cgroup/cpu.max", "/sys/fs/cgroup/memory.max"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if strings.HasSuffix(path, "cpuinfo") {
			for _, line := range lines {
				if strings.HasPrefix(line, "model name") {
					fmt.Fprintln(&b, line)
					break
				}
			}
		} else if strings.HasSuffix(path, "meminfo") {
			fmt.Fprintln(&b, lines[0])
		} else {
			fmt.Fprintf(&b, "%s: %s\n", path, strings.TrimSpace(string(data)))
		}
	}
	if runtime.GOOS == "darwin" {
		for _, key := range []string{"machdep.cpu.brand_string", "hw.memsize"} {
			if out, err := exec.Command("sysctl", "-n", key).Output(); err == nil {
				fmt.Fprintf(&b, "%s: %s", key, out)
			}
		}
	}
	if out, err := exec.Command("git", "rev-parse", "HEAD").Output(); err == nil {
		fmt.Fprintf(&b, "Checkout HEAD: %s", out)
	}
	if out, err := exec.Command("git", "status", "--porcelain").Output(); err == nil {
		fmt.Fprintf(&b, "Uncommitted changes: %t\n", len(out) > 0)
	}
	fmt.Fprintln(&b, "Hardware describes the execution environment, possibly a container, not a remote user's computer. CPU/memory limits may differ from host totals. Unavailable fields are omitted.")
	return b.String()
}
