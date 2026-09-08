# Personal-use search benchmark

Run explicitly from the repository root with Go 1.25 or later:

```sh
make benchmark
```

The default run creates separate indexes for **100, 500 and 2,000 documents**:
small active folders, a household archive, and a larger personal/project archive.
Each corpus contains approximately equal numbers of generated plain-text files
and real text-layer PDFs covering ten family/project topics. No personal data,
external service, model download, Python, or additional dependency is needed.
The existing PDFium and SQLite dependencies are used.

For a quick smoke run or a different archive size:

```sh
make benchmark BENCHMARK_SIZES=20 BENCHMARK_REPEATS=2
make benchmark BENCHMARK_SIZES=250,1000 BENCHMARK_REPEATS=50
```

Sizes must be unique integers from 20 through 10,000. Repetitions must be from
1 through 1,000. The 20-document minimum includes every topic in both formats.
The benchmark has a `benchmark` Go build tag: ordinary tests, builds, `make
verify`, and `make verify-full` do not run it. The Make target first runs the
benchmark's own correctness tests, then runs the measurements.

## What is measured

| Mode | Production path | Timed work |
|---|---|---|
| Full-text only | `Store.Candidates`, zero query vector | SQLite FTS5/BM25 and source text fetching, up to 40 candidates; vector SQL skipped |
| Vector only | `Store.Candidates`, empty FTS query | SQLite vec0 cosine search and source text fetching, up to 40 candidates; FTS SQL skipped |
| Hybrid | `Retriever.Retrieve` | Query embedding, both searches, source text fetching and rank fusion, top 10 results |

The first two modes use precomputed query vectors to isolate database retrieval.
They share the production candidate-fetching path for comparable timing scopes.
The hybrid measurement reflects the normal retrieval API. It has a different
result limit and includes more work, so it is not a like-for-like SQL comparison.
Application code and normal search behavior are unchanged.

Each of ten queries gets three untimed warmups and, by default, 30 measured
repetitions per mode. Modes rotate within each repetition. The Markdown report
includes minimum, median, p95 (nearest rank), maximum, mean and returned-result
counts per query/mode. Indexing time and indexed document/chunk counts are also
reported. Indexing failures, integrity failures, or empty query results abort
the run instead of producing a successful report.

This is a **single-client, warm-cache retrieval benchmark**. It excludes CLI
startup, reconciliation, printing, and initial indexing from search latency.
`chiedi search` reconciles first, so its total response time can be higher.
Index time excludes corpus generation and database/root setup; it includes PDF
extraction, chunking, embedding and persistence. These short English synthetic
records do not represent scans, OCR, long documents, multilingual relevance,
cold storage, or concurrent enterprise workloads. Nonempty results are a sanity
check, not a semantic quality score.

## Output and hardware

Every run writes a new `benchmarks/output/<UTC timestamp>-<unique suffix>/`:

- `results.md`: measurements, methodology and hardware/runtime information.
- `<count>/documents/`: generated PDF and text corpus.
- `<count>/index.db`: isolated database, with any SQLite sidecars.
- `FAILED.txt`: failure details if a scenario aborts; no successful report.

The **entire output folder is Git-ignored**. Benchmark source code, its tests, these instructions,
and explicitly selected reports under `benchmarks/samples/` belong in Git. Output is never indexed as a user root,
and existing user databases are never opened. Repeated runs do not overwrite
previous results. Remove unwanted run directories manually to reclaim space.

Reports identify OS/architecture, Go version, visible logical CPUs, GOMAXPROCS,
CPU model and RAM where available, Linux cgroup limits where available, and Git
revision/dirty status. Hardware describes the machine executing the benchmark;
when run in a hosted container it does not describe your laptop. Host RAM and
CPU counts can exceed container limits. Run it on your own computer for local
performance numbers, and avoid other heavy work during measurement.

## Sample result

See [the Linux / Xeon Platinum 8573C sample](samples/2026-09-08-linux-xeon-8573c.md)
for the 100, 500 and 2,000-document run, including vector-only timings and
execution-machine hardware. This selected report is committed as an example;
new runs continue to write only to the ignored output directory.


## Retrieval accuracy

`make benchmark` also runs an **untimed quality pass** against the same index.
No application retrieval, model, corpus text or latency workload is changed.
The existing deterministic `record-00000` filenames are the stable document
identifiers; SQLite document/chunk IDs are deliberately not used as ground truth.

The versioned suite has 40 queries for every corpus size:

- 20 exact references, each relevant to one document: ten evenly spaced pairs
  across the archive, covering both TXT and PDF.
- 10 topic queries, relevant to every document generated for that topic.
- 10 manually specified paraphrases with the same topic judgments. These mix
  shared vocabulary and alternative wording; they are not a blind semantic test.

Judgments are derived from the corpus specification before retrieval, never
from model scores or observed search hits. `ground-truth.json` records the query
text, category, relevant IDs, relative file paths and SHA-256 of each corpus file.
Its hash is included in the Markdown report. `quality-results.json` records
ordered unique document IDs and unrounded per-query metrics for all three modes.
Both files live under each `<count>/` directory in the ignored output folder.

| Metric | Definition |
|---|---|
| Recall@1 / @5 / @10 | Relevant documents in the first k unique retrieved documents divided by **all** relevant documents for that query |
| MRR (40-chunk pool) | Mean of 1 / rank of the first relevant unique document; zero if none is returned |

The report gives macro averages over queries, both overall and by category.
Overall results weight each query equally (20 exact, 10 topic, 10 paraphrase),
not each category equally. A topic can have more than ten relevant documents:
its Recall@10 ceiling is then 10 / relevant-count. Compare category results and
corpus sizes with that denominator in mind; recall is not a hit-rate metric.

Full-text and vector search use the existing isolated `Store.Candidates` paths,
with 40 candidates. The evaluator sorts by `LexicalRank` or `VectorRank`, since
candidate rows themselves arrive in chunk-ID order. Hybrid uses unmodified
`Retriever.Retrieve` with a quality-only return limit of 40 (latency still uses
10), preserving its production ranking and 40-candidate budget per path.
Duplicate document chunks retain only their first occurrence, before applying
Recall cutoffs. There is no refill after deduplication. MRR covers only the
returned pool, **not exhaustive corpus MRR**. Missing results score zero;
retrieval or invalid-ground-truth errors abort the benchmark.

Reproduce with the same checkout, corpus size, Go version and dependency versions.
Corpus and query generation use no randomness, network or clock. Ranking ties
retain production behavior; byte-identical cross-platform floating-point rankings
are not guaranteed. Tests rebuild separate fresh indexes and compare the quality
JSON artifacts byte for byte. Timing, hardware and timestamp fields in results.md
are naturally machine/run dependent.

Run only the benchmark correctness tests with:

```sh
go test -tags benchmark -count=1 ./benchmarks/search
```

Tests include known metric examples, chunk-to-document deduplication, SQL rank
ordering, mode isolation, fixture validation, macro averaging and repeatability
through real PDF extraction/indexing/retrieval. Quality scores are reported as
observations, not enforced as arbitrary pass/fail thresholds. The previous
committed latency sample predates these quality measurements.
