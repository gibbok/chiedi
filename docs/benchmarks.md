# Search benchmarks

The opt-in benchmark measures search latency and document retrieval accuracy on
repeatable synthetic corpora. Use it to compare changes on the same machine, then
validate against documents representative of your intended use.

[Documentation index](README.md)

## Run and outputs

```sh
make benchmark
make benchmark BENCHMARK_SIZES=20 BENCHMARK_REPEATS=2
```

- Default sizes: **100, 500, 2,000 documents**, with **30 measured repetitions** per
  query/mode. Sizes must be unique in 20–10,000; repetitions in 1–1,000.
- Approximately half TXT and half real text-layer PDFs, cycling through ten topics,
  with 3–7 sections per document and stable record references. No OCR or personal data.
- `benchmark` build tags exclude the harness from normal tests/builds and both
  verification gates. The Make target runs benchmark correctness tests first.
- Each run creates `benchmarks/output/<UTC timestamp>-<unique suffix>/`, entirely
  Git-ignored. It never opens the user's configured database.
- Outputs: `results.md`; per-size `documents/`, `index.db`, `ground-truth.json`,
  and `quality-results.json`. Failed scenarios write `FAILED.txt` instead of a successful report.
- Reports record OS/architecture, Go, visible CPU/RAM and Linux cgroup limits where
  available, GOMAXPROCS, and Git revision/dirty state. Hosted hardware is not your laptop.

The [committed latency sample](../benchmarks/samples/2026-09-08-linux-xeon-8573c.md)
predates accuracy reporting. New results remain ignored unless deliberately selected.

## Latency methodology

| Mode | Measured path | Limit |
| --- | --- | --- |
| Full-text | `Store.Candidates` with a zero vector; FTS5/BM25 and text fetching | Up to 40 chunks |
| Vector | `Store.Candidates` with empty FTS query; cosine search and text fetching | Up to 40 chunks |
| Hybrid | `Retriever.Retrieve`; query embedding, both searches, text fetching, fusion | Top 10 chunks |

- Ten queries, three untimed warmups per query/mode; rotate modes within each
  measured repetition. Report minimum, median, nearest-rank p95, maximum, mean,
  and returned-result counts.
- Full-text/vector timings exclude query embedding; hybrid includes it and uses a
  different result limit. These are not identical timing scopes.
- Single client, persistent connection, warm caches; exclude process startup,
  reconciliation, initial indexing, and printing from search timings.
- Report indexing separately: extraction, chunking, embedding, and persistence;
  exclude corpus generation and database/root setup.
- Indexing/integrity failures or empty latency results abort the run. Nonempty
  results alone do not establish relevance.

## Accuracy algorithm

A separate untimed pass evaluates the same index with **60 queries**:

- **20 exact-reference queries:** one TXT/PDF pair per topic, sampled across the
  corpus; each has one relevant document.
- **10 topic queries:** all documents assigned to that topic are relevant.
- **10 fixed paraphrases:** use the same topic judgments with alternative wording.
- **10 Italian-to-English queries:** use the same topic judgments.
- **10 Czech-to-English queries:** use the same topic judgments.

Judgments come from the corpus specification, not retrieved scores. Stable
`record-00000` identifiers avoid depending on SQLite IDs. Ground truth records
paths and each file's SHA-256; its own hash is included in the report.

1. Retrieve up to 40 chunks per isolated mode, or 40 fused chunks for hybrid
   (hybrid still uses 40 candidates per channel internally).
2. Order isolated modes by their SQL lexical/vector ranks, not candidate row order.
3. Convert paths to stable document IDs and keep only the first occurrence of each
   document. Deduplicate **before** applying metric cutoffs; do not refill.
4. Compute per-query metrics, then macro-average overall and by category.

| Metric | Definition |
| --- | --- |
| Recall@k (`k = 1, 5, 10`) | Relevant documents among the first k unique results / all relevant documents |
| Reciprocal rank | `1 / rank` of the first relevant unique document, or 0 if absent |
| MRR | Mean reciprocal rank over the evaluated queries within the returned pool |

Overall averages weight queries equally: 20 exact, 10 topic, 10 paraphrase,
10 Italian-to-English and 10 Czech-to-English, so categories do not have equal weight. If a topic has 100 relevant documents,
Recall@10 cannot exceed 0.10. MRR is limited to the returned 40-chunk pool,
not exhaustive corpus MRR. Missing hits score zero; invalid judgments or retrieval
errors abort the run. Quality scores have no arbitrary pass/fail threshold.

## Reproducibility and interpretation

- Corpus/query generation uses no randomness, clock, or network. Match checkout,
  corpus size, Go, and dependency versions when comparing runs.
- Production SQL ties and floating-point behavior remain; identical cross-platform
  rankings are not guaranteed. Timing/hardware fields naturally vary.
- Correctness tests rebuild fresh indexes and compare quality JSON artifacts, and
  check metric examples, document deduplication, rank order, mode isolation,
  sampling, and macro averages:

```sh
make setup
CHIEDI_ASSETS="$PWD/bin/assets" go test -p 1 -tags benchmark -count=1 ./benchmarks/search
```

Synthetic short English documents do not establish quality or latency for scans,
long reports, multilingual corpora, cold disks, or concurrent users. CLI latency
also includes reconciliation. See [retrieval](retrieval.md) for ranking limitations.

Implementation: [benchmark runner](../benchmarks/search/main.go),
[quality evaluator](../benchmarks/search/quality.go),
[quality tests](../benchmarks/search/quality_test.go).

## E5 measurements

The production model is now multilingual E5-small INT8. Query-only and hybrid
paths both use the query prefix; indexed passages use the passage prefix.
The report includes model initialization plus first-query time separately from
warm retrieval timings. Quality v3 adds ten Italian-to-English and ten
Czech-to-English queries with topic judgments independent of model outputs.
Existing English queries and judgments are retained (60 queries, three modes).
Older projection results under `benchmarks/samples` are historical baselines;
they do not describe E5 latency or relevance.
