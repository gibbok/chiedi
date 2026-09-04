# local-genius (`docdex`)

`docdex` is a local-first semantic document retrieval engine for Codex. It indexes `.txt`, `.md`, and text-layer `.pdf` files into one SQLite database, combines local vector similarity with FTS5, and exposes grounded evidence through MCP over stdio.

No document content, query, or embedding leaves the machine. The built-in 384-dimensional semantic projection model requires no model server or network access.

## Quick start

```bash
go build -o docdex ./cmd/docdex
export DOCDEX_DB="$PWD/docdex.db"
./docdex init
./docdex add ~/Documents
./docdex index
./docdex search "How many vacation days do employees receive?"
./docdex status
```

Configure Codex to run the MCP server with the same database path:

```bash
DOCDEX_DB=/absolute/path/docdex.db /absolute/path/docdex mcp
```

MCP tools:

- `retrieve`: hybrid semantic and exact-term evidence retrieval
- `read_chunks`: fetch selected chunks plus bounded neighbors
- `list_documents`: inspect indexed sources
- `index_status`: inspect roots, counts, failures, and last incremental work

Every retrieval reconciles configured roots first, so edits, renames, moves, additions, and deletions become visible even if operating-system notifications were missed. An unchanged reconciliation performs no extraction or embedding work.

## Commands

```text
docdex init
docdex add <directory>
docdex remove <directory>
docdex roots
docdex index
docdex reconcile
docdex search [--limit N] <question>
docdex status
docdex doctor
docdex mcp
docdex version
```

Use `--db PATH` before the command, or set `DOCDEX_DB`. The default is `docdex.db` in the current directory.

## Limits

V1 indexes UTF-8 text, Markdown, and PDFs with an extractable text layer. It does not perform OCR. The compact built-in semantic model is English-oriented and intentionally replaceable through the `embedding.Embedder` interface.

## Verification

```bash
make verify
make verify-full
```

The full gate builds the real executable and exercises initialize → add → index → semantic search, exact retrieval, MCP calls, incremental update, rename, delete, reconciliation, and restart.
