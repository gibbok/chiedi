# chiedi

`chiedi` is a local-first semantic document retrieval engine for Codex. It indexes `.txt`, `.md`, and text-layer `.pdf` files into one SQLite database, combines local vector similarity with FTS5, and exposes grounded evidence through MCP over stdio.

No document content, query, or embedding leaves the machine. Database files use owner-only filesystem permissions. The built-in 384-dimensional feature-hashing embedder requires no model server or network access.

Requires Go 1.25 or newer. SQLite-vec is bundled through the pure-Go SQLite driver; no C compiler, extension download, or runtime service is needed.

## Quick start

```bash
go build -o chiedi ./cmd/chiedi
export CHIEDI_DB="$PWD/chiedi.db"
./chiedi init
./chiedi add ~/Documents
./chiedi index
./chiedi search "How many vacation days do employees receive?"
./chiedi status
```

Configure Codex to run the MCP server with the same database path:

```bash
CHIEDI_DB=/absolute/path/chiedi.db /absolute/path/chiedi mcp
```

MCP tools:

- `retrieve`: hybrid semantic and exact-term evidence retrieval
- `read_chunks`: fetch selected chunks plus bounded neighbors
- `list_documents`: inspect indexed sources
- `index_status`: inspect roots, counts, failures, and last incremental work

Document and chunk results include both `root_id`/`root_path` and the path relative to that root. This keeps sources unambiguous when two configured directories contain the same relative filename. CLI search results print the resolved source path.

Every retrieval reconciles configured roots first, so edits, renames, moves, additions, and deletions become visible even if operating-system notifications were missed. An unchanged reconciliation performs no extraction or embedding work.

## Commands

```text
chiedi init
chiedi add <directory>
chiedi remove <directory>
chiedi roots
chiedi index
chiedi reconcile
chiedi search [--limit N] <question>
chiedi status
chiedi doctor
chiedi mcp
chiedi version
```

Use `--db PATH` before the command, or set `CHIEDI_DB`. The default is `chiedi.db` in the current directory.

## Locating failed documents

`chiedi index` and `chiedi reconcile` return `failed_documents` (failures
encountered in that run) and `failed_document_paths` (their absolute paths).
An unchanged run reports zero and `[]`, even when previously failed files remain.

For **all currently stored failures**, use `chiedi status` or MCP
`index_status`: `counts.failed_count` is the total and the top-level
`failed_document_paths` array contains the matching absolute paths, ordered
by root and relative filename. These paths remain visible across unchanged
runs and use the canonical registered root (symlinks in the root are resolved).
The nested `last_reconciliation` describes only the most recent run.

MCP `index_status` reconciles first; CLI `status` reads the stored state
without rescanning. After fixing, renaming, or deleting a file, run
`chiedi reconcile` before CLI `status`. Use MCP `list_documents` with
`{"status":"failed"}` to inspect failure reasons. No database rebuild is
needed to show current failure paths from an existing index.

## Limits

V1 indexes UTF-8 text, Markdown, and PDFs. PDFium handles PDF text decoding and word spacing; optional local Tesseract OCR reads scanned pages while preserving page citations. See [PDF conversion](docs/pdf-conversion.md) for installation, language settings, limits, and upgrading an existing index. The built-in embedder uses deterministic feature hashing, stemming, and a curated English synonym dictionary. It is not a trained language model and does not provide general semantic understanding. The `embedding.Embedder` interface permits a future local model.

Embedding model identities are stored with the index. If an upgrade changes the projection algorithm, `chiedi` rejects the older vectors instead of mixing incompatible embeddings. Rebuild into a new database, or remove the old database and run `init`, `add`, and `index` again.

## Verification

For a human-readable walkthrough that creates its own temporary documents and exercises the CLI and MCP:

```bash
make demo
```

For the complete automated gate:

```bash
make test-all
```

The complete gate verifies dependencies, unit and integration tests, race detection, static analysis, the production build, repeated state-sensitive tests, and a real-binary acceptance scenario. That scenario covers initialize → add → index → semantic search, exact search, text-layer PDF extraction, every MCP tool, concurrent processes, incremental update, rename reuse, delete, reconciliation, malformed input, restart, and `doctor`. Unit regressions also cover transactional rollback, filesystem failures, symlink containment, literal path filters, empty JSON collections, multi-root provenance, malformed/non-finite vectors, stale FTS rows, strict MCP arguments, bounded file reads, and 50 dictionary/identifier regression cases and separate document-level retrieval cases.

Run `make help` to see individual targets. See [Testing chiedi](docs/testing.md) for expected behavior, preserved demo files, focused commands, and failure interpretation.

## Vector storage and evidence consistency

SQLite-vec `vec0` stores the vectors and performs exact cosine nearest-neighbor queries inside SQLite. Path filters apply before selecting nearest neighbors. FTS5 ranks, vector ranks, and candidate source text are read from one database snapshot; Go only receives the bounded candidate union. This is exact scanning, not an approximate nearest-neighbor index; query cost still grows with corpus size.

Schema 1 databases are migrated transactionally to schema 2 without re-embedding. Chunk IDs are never reused after replacement or deletion. If a document changes between `retrieve` and `read_chunks`, the latter reports a stale reference; retrieve again. Collection tools return an object containing a `results` array in both MCP structured content and text content.

Only regular source files are indexed. Named pipes, devices, sockets, and symbolic links are excluded.
