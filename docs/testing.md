# Testing chiedi

The repository provides two primary ways to validate the application:

```bash
make demo
make test-all
```

Use `make demo` when you want to see the application working. Use `make test-all` before opening, reviewing, or merging a pull request.

## Prerequisites

- Go 1.25 or newer
- GNU Make
- Bash for the interactive demo
- No C compiler is required; the SQLite driver is implemented in pure Go
- No model server, cloud account, API token, Docker daemon, or network service is required at runtime

The first Go build may download the modules recorded in `go.sum`. After the module cache is populated, the application and tests run locally without a document-processing service or model download.

## Quick interactive demonstration

Run:

```bash
make demo
```

The command builds `bin/chiedi`, creates an isolated temporary directory, and prints each operation and its result. It verifies:

1. database initialization;
2. root registration;
3. TXT and Markdown indexing;
4. unsupported-file exclusion;
5. status and root inspection;
6. semantic retrieval using different wording;
7. exact-reference retrieval;
8. MCP initialization and tool discovery;
9. MCP `retrieve`, `read_chunks`, `list_documents`, and `index_status` calls;
10. automatic reconciliation after a document edit;
11. rename detection without re-embedding;
12. deletion cleanup;
13. malformed-PDF isolation;
14. restart-compatible retrieval;
15. database health checking;
16. root removal.

The temporary files are deleted after a successful or failed run. Preserve them for inspection with:

```bash
KEEP_DEMO=1 make demo
```

Use a specific directory instead with:

```bash
CHIEDI_DEMO_DIR=/tmp/my-chiedi-demo KEEP_DEMO=1 make demo
```

When `CHIEDI_DEMO_DIR` is supplied, the directory must be empty and the script never deletes it.

## Complete automated verification

Run:

```bash
make test-all
```

This executes the following checks sequentially:

| Make target | What it proves |
|---|---|
| `make deps` | Downloaded modules match the committed checksums |
| `make test-unit` | Unit, integration, protocol, extraction, storage, and indexing tests pass |
| `make test-race` | Concurrent test execution has no detected data races |
| `make test-vet` | Go static analysis reports no problems |
| `make build` | The real `bin/chiedi` executable builds |
| `make test-repeat` | Incremental indexing and retrieval remain stable across repeated runs |
| `make test-e2e` | The compiled binary passes the complete functional acceptance scenario |
| `make demo` | The documented CLI and MCP walkthrough works exactly as shown |

`make verify` and `make verify-full` remain aliases for `make test-all` so CI and older local workflows use the same gate.

The unit layer includes edge-case regressions for empty databases, owner-only database permissions, transactional rollback and restart recovery, concurrent SQLite writers, duplicate relative paths across roots, case-sensitive literal path prefixes containing `%` or `_`, metadata-only and same-timestamp file replacement, cross-format rename, unavailable roots, incomplete permission-constrained scans, symlink containment, cancellation during embedding, chunk overlap, malformed query vectors, non-finite vector values, vector/FTS corruption, oversized reads, strict MCP JSON decoding, invalid tool bounds, and corrupt status metadata.

Retrieval also has a 50-case quality gate: 40 curated-dictionary synonym regressions and 10 exact-identifier cases. Every expected source must rank first. These cases validate known mappings, not general semantic understanding. Separate multi-document fixtures exercise realistic questions without extending the synonym dictionary.

## End-to-end acceptance test

For a focused, verbose acceptance run:

```bash
make test-e2e
```

Unlike the shell demonstration, this test creates a valid text-layer PDF and verifies PDF extraction and page provenance. It also tests semantic and exact retrieval, MCP protocol output, incremental embedding counts, rename reuse, deletion, reconciliation idempotence, malformed-PDF containment, restart behavior, and `doctor` using the actual compiled executable.

## Individual checks

During development, run a narrower target:

```bash
make test-unit
make test-race
make test-vet
make test-repeat
make build
```

List all supported targets at any time:

```bash
make help
```

## Manual use with your own documents

Keep experiments separate from your normal data by selecting an explicit database:

```bash
make build
export CHIEDI_DB="$PWD/manual-test.db"
./bin/chiedi init
./bin/chiedi add /absolute/path/to/test-documents
./bin/chiedi index
./bin/chiedi status
./bin/chiedi search "your question"
./bin/chiedi doctor
```

## Codex host smoke test

The automated E2E test launches the production MCP binary, completes initialization, sends the initialized notification, pings it, discovers tools, and calls every tool over stdio. A final host-level check requires a locally authenticated Codex installation and therefore is intentionally manual:

1. configure Codex to launch `/absolute/path/bin/chiedi mcp` with `CHIEDI_DB` set to the tested database;
2. restart or refresh MCP connections in Codex;
3. confirm the `retrieve`, `read_chunks`, `list_documents`, and `index_status` tools are visible;
4. ask Codex to retrieve a known exact identifier and a semantic paraphrase from the corpus;
5. confirm its answer cites the returned root, relative path, heading, or PDF page as applicable.

No API token or per-token OpenAI API configuration is required for `chiedi`; it communicates with the authenticated Codex host over local stdio.

Remove `manual-test.db`, `manual-test.db-shm`, and `manual-test.db-wal` when the experiment is no longer needed.

## Reading failures

- A non-zero command exit means the gate failed.
- `failed_documents` may be non-zero when a fixture intentionally contains a malformed PDF; healthy documents must remain searchable.
- `embedding_jobs` must be zero after a pure rename or unchanged reconciliation.
- After a one-chunk edit, the tests require only the affected chunk to be embedded.
- Any race-detector, vet, build, protocol, or acceptance failure blocks completion.

## Pre-review regressions

The suite verifies SQLite-vec availability and filtered nearest neighbors (including a relevant source outside the global top 40), vector cleanup on rename/delete/root removal, dimension rejection and rollback, schema-1 migration, non-reused chunk IDs, a writer committing during a retrieval snapshot, FIFO exclusion, MCP object-shaped structured content, and stale references between tool calls. The real-binary MCP check asserts every tool succeeds and returns an object, in addition to checking protocol framing.

SQLite-vec remains an exact scan. The document fixtures are regression evidence, not a general semantic benchmark or a corpus-scale latency guarantee.

## Opt-in performance benchmark

Run `make benchmark` for generated personal-use corpora of 100, 500 and 2,000
PDF/text documents, measuring full-text-only, vector-only and hybrid retrieval.
It is intentionally excluded from all normal verification targets.
See [the benchmark guide](../benchmarks/README.md) for methodology, parameters
and Git-ignored Markdown reports with execution-machine hardware details.


The same opt-in benchmark now includes deterministic document retrieval accuracy:
Recall@1/5/10 and MRR within the returned 40-chunk pool, overall and by exact-reference,
topic and paraphrase category. It writes ground-truth and per-query ranking JSON
beside the index. Run `go test -tags benchmark -count=1 ./benchmarks/search` for
metric and fresh-index reproducibility tests without the full performance run.
