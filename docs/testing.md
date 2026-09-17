# Development and testing

Use the demo to understand the workflow and the verification gates to check a
change. Tests exercise local extraction, storage, retrieval, CLI, and MCP behavior.

[Documentation index](README.md)

## Setup and contribution workflow

- Go 1.25+, GNU Make, and Bash; initial module downloads may need network access.
- Production embeddings require cgo and a C/C++ compiler. The Go setup command prepares build
  assets; see [native installation](embedding.md).
- Install Tesseract with English data for real OCR acceptance tests.
- Follow [AGENTS.md](../AGENTS.md): preserve local processing, provenance, atomic
  document storage, and embedding reuse; add tests for behavior changes.
- Prefer the Go standard library and document new dependencies in
  [DEPENDENCIES.md](../DEPENDENCIES.md), the maintained dependency/license inventory.
- Direct dependencies: `modernc.org/sqlite` (SQLite/FTS5/vec), `go-pdfium` (PDFium),
  and wazero (WASM runtime). Exact versions are pinned in [go.mod](../go.mod) and
  checksums in [go.sum](../go.sum).

Before completion, run both repository-required gates:

```sh
CHIEDI_TEST_OCR=1 make verify
CHIEDI_TEST_OCR=1 make verify-full
```

Both are aliases for `test-all` and execute the same full sequence. CI runs
`make verify` once per platform with OCR required and checks that `go mod tidy`
leaves dependency files unchanged. Packaging and relocated-installation checks
run on Linux, and on macOS for tags or an explicit manual option. Without
`CHIEDI_TEST_OCR=1`, real OCR acceptance coverage is skipped; other PDF regressions
still run.

## Verification targets

| Target | Purpose |
| --- | --- |
| `make test-install` | Check relative/shared prefixes, running-process upgrades and failed-copy recovery without downloading models |
| `make deps` | Verify cached modules against checksums |
| `make test-unit` / `make test` | Run `go test ./...`, including compiled-binary tests; real OCR remains opt-in |
| `make test-race` / `make race` | Run all default tests under the race detector |
| `make test-vet` / `make vet` | Run Go static analysis |
| `make build` | Build `bin/chiedi` |
| `make test-repeat` | Run indexer and retrieval tests three times |
| `make test-e2e` | Run compiled-binary acceptance tests verbosely with caching disabled |
| `make demo` | Build and exercise CLI/MCP against a temporary corpus |
| `make test-all` | Run all of the above primary targets sequentially |
| `make benchmark` | Separate opt-in latency/accuracy workload; excluded from verification |
| `make clean` | Remove `bin/` build output |
| `make help` | List developer commands |

`test-e2e` sets `CHIEDI_E2E=1`, but the current tests do not read that variable.
The common binary scenario already runs under ordinary `go test ./...` unless
`-short` is supplied; the PDF/OCR binary scenario requires `CHIEDI_TEST_OCR=1`.

## What the tests cover

- **Installation:** relative paths with spaces, published release permissions, upgrades
  while current or legacy executables run, and preservation after failed asset copies.
- **Indexing:** unchanged files, single-chunk edits, rename reuse, deletion,
  metadata-only changes, unavailable roots, incomplete scans, and symlink containment.
- **Storage:** rollback, concurrent writers, schema migration, vector dimensions
  and non-finite values, FTS/vector consistency, and non-reused chunk IDs.
- **Retrieval:** literal path filters, snapshot consistency, synonym and
  identifier regressions, and separate document-level questions.
- **MCP:** framing, strict arguments, tool schemas/results, bounded neighbors,
  failure locations, and stale references.
- **PDFs:** digital and mixed scanned documents, original page citations, malformed
  files, bounded extraction, cancellation, and OCR errors/reuse.
- **Acceptance:** build a real executable and drive indexing, search, all MCP
  tools, concurrent processes, incremental changes, restart, and `doctor`.

The 50-case synonym/identifier suite checks 40 legacy synonym relationships using explanatory passages and disambiguating query context and 10
identifiers, requiring the expected source to rank first. It is a regression gate,
not evidence of general semantic understanding. See [benchmarks](benchmarks.md)
for document-level accuracy measurements.

## Demo and manual client check

```sh
make demo
KEEP_DEMO=1 make demo
CHIEDI_DEMO_DIR=/tmp/my-chiedi-demo KEEP_DEMO=1 make demo
```

The demo normally deletes its temporary files. `KEEP_DEMO=1` preserves them;
a supplied `CHIEDI_DEMO_DIR` must be empty and is never removed by the script.

For a real MCP host smoke test:

1. Build and index a small corpus using [the quick start](getting-started.md).
2. Configure the client with the same absolute database path and [MCP command](mcp.md).
3. Confirm all four tools are available; retrieve a known reference and a paraphrase.
4. Expand a returned chunk and check its root/path, heading, and page citations.

## Interpreting failures

- Nonzero gate exits block verification; do not weaken a failing test.
- Deliberately malformed fixtures can have stored failed documents while healthy
  documents remain searchable.
- Unchanged reconciliations and pure renames should have zero embedding jobs.
- Keep test documents/databases out of Git. Benchmark output is ignored separately;
  explicitly selected sample reports live under `benchmarks/samples/`.

Implementation: [Makefile](../Makefile), [CI](../.github/workflows/verify.yml),
[demo](../scripts/demo.sh), [E2E](../internal/e2e/e2e_test.go).

## Native E5 coverage

All embedding-dependent tests now exercise the real bundled multilingual E5 model.
Make prepares pinned assets and sets `CHIEDI_ASSETS` for tests and their CLI/MCP
subprocesses. For direct `go test`, run `make setup` first and export
`CHIEDI_ASSETS="$PWD/bin/assets"`. No tests fetch model assets themselves.

CI verifies Linux x64 and macOS ARM; a separate workflow benchmarks E5 and
retrieval on both. Draft PRs and documentation-only changes do not trigger these
jobs. The installation smoke test checks symlink asset discovery from another
working directory. Go test package parallelism is limited to one for predictable
native model/PDF memory use; race detection remains enabled.
