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

The **entire output folder is Git-ignored**. Only benchmark source code, its tests,
and these instructions belong in Git. Output is never indexed as a user root,
and existing user databases are never opened. Repeated runs do not overwrite
previous results. Remove unwanted run directories manually to reclaim space.

Reports identify OS/architecture, Go version, visible logical CPUs, GOMAXPROCS,
CPU model and RAM where available, Linux cgroup limits where available, and Git
revision/dirty status. Hardware describes the machine executing the benchmark;
when run in a hosted container it does not describe your laptop. Host RAM and
CPU counts can exceed container limits. Run it on your own computer for local
performance numbers, and avoid other heavy work during measurement.
