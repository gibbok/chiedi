# Testing docdex

The repository provides two primary ways to validate the application:

```bash
make demo
make test-all
```

Use `make demo` when you want to see the application working. Use `make test-all` before opening, reviewing, or merging a pull request.

## Prerequisites

- Go 1.23 or newer
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

The command builds `bin/docdex`, creates an isolated temporary directory, and prints each operation and its result. It verifies:

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
DOCDEX_DEMO_DIR=/tmp/my-docdex-demo KEEP_DEMO=1 make demo
```

When `DOCDEX_DEMO_DIR` is supplied, the directory must be empty and the script never deletes it.

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
| `make build` | The real `bin/docdex` executable builds |
| `make test-repeat` | Incremental indexing and retrieval remain stable across repeated runs |
| `make test-e2e` | The compiled binary passes the complete functional acceptance scenario |
| `make demo` | The documented CLI and MCP walkthrough works exactly as shown |

`make verify` and `make verify-full` remain aliases for `make test-all` so CI and older local workflows use the same gate.

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
export DOCDEX_DB="$PWD/manual-test.db"
./bin/docdex init
./bin/docdex add /absolute/path/to/test-documents
./bin/docdex index
./bin/docdex status
./bin/docdex search "your question"
./bin/docdex doctor
```

Remove `manual-test.db`, `manual-test.db-shm`, and `manual-test.db-wal` when the experiment is no longer needed.

## Reading failures

- A non-zero command exit means the gate failed.
- `failed_documents` may be non-zero when a fixture intentionally contains a malformed PDF; healthy documents must remain searchable.
- `embedding_jobs` must be zero after a pure rename or unchanged reconciliation.
- After a one-chunk edit, the tests require only the affected chunk to be embedded.
- Any race-detector, vet, build, protocol, or acceptance failure blocks completion.
