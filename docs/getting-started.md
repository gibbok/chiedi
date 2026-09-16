# Getting started and CLI

Choose folders to index, then search their contents from the terminal. Chiedi
builds its own index without changing your source documents.

[Documentation index](README.md)

## Build and first search

- Requires Go 1.25 or newer. The first build may download Go modules.
- GNU Make and Bash are used by the developer commands and demo.
- Native embeddings require Go and a C/C++ compiler at build time. Run
  `make install` for a complete installation under `~/.local`, or `make build`
  for a local bundle. See [native installation](embedding.md).
- Scanned PDFs additionally need local Tesseract and language data.
  See [PDF conversion](pdf-conversion.md).

Run from the repository root:

```sh
make build
export CHIEDI_DB="$PWD/chiedi.db"
./bin/chiedi init
./bin/chiedi add /absolute/path/to/documents
./bin/chiedi index
./bin/chiedi search "database migration"
./bin/chiedi status
```

`add` registers a directory; `index` scans all registered roots. An extra directory
argument to `index` is currently ignored: it neither registers nor limits the scan
to that directory. Use `add` first. Search also scans first, so an explicit index
is optional before subsequent searches.

## Command reference

| Command | Behavior |
| --- | --- |
| `init` | Open/create the database, apply schema setup, and print its absolute path |
| `add <directory>` | Register a canonical absolute root, resolving root symlinks |
| `remove <directory>` | Remove that root and its indexed data; source files stay on disk |
| `roots` | Print registered roots as JSON |
| `index` / `reconcile` | Run the same incremental reconciliation and print run statistics as JSON |
| `search [--limit N] [--path-prefix PREFIX] <question>` | Reconcile, then print ranked passages with resolved paths and available headings/pages |
| `status` | Print stored counts, failure paths, and the last reconciliation without rescanning |
| `doctor` | Check SQLite integrity, embedding compatibility, and FTS/vector consistency |
| `mcp` | Serve MCP on stdin/stdout; see [MCP integration](mcp.md) |
| `version` | Print the application version without opening a database |

- Database precedence: `--db PATH` → `CHIEDI_DB` → `chiedi.db` in the working directory.
- Put `--db` **before** the command; put search flags **before** the question.
- Search defaults to 10 chunks; accepts 1–50, or 0 for the default.
- `--path-prefix` is a case-sensitive, literal prefix of the root-relative path
  across all roots. Backslashes normalize to `/`; `%` and `_` are literal.
  Use `notes/` to restrict to that directory rather than every name starting `notes`.
- Most commands open/create the selected database. Check the path when an index
  unexpectedly appears empty.

```sh
./bin/chiedi --db /absolute/path/index.db search --limit 5 --path-prefix notes/ "release checklist"
```

## Process configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `CHIEDI_ASSETS` | `assets/` beside the resolved executable | Optional explicit model/runtime directory; normally unnecessary |
| `CHIEDI_DB` | `chiedi.db` | SQLite index location |
| `CHIEDI_PDF_OCR` | `auto` | PDF OCR mode: `auto`, `always`, or `off` |
| `CHIEDI_OCR_LANG` | `eng` | Installed Tesseract languages, optionally joined with `+` |

Set the same database and OCR settings on CLI and MCP processes. OCR settings do
not invalidate unchanged files; see [rebuilding](storage.md#rebuilding-and-upgrades).

Diagnostics go to stderr. Invalid global/search flag parsing, missing commands,
and unknown commands exit 2; operational errors exit 1. A run can record failed
documents without a nonzero exit, so inspect its statistics as well.

Implementation: [CLI dispatch](../internal/app/app.go),
[root management](../internal/store/store.go), [OCR options](../internal/extract/pdf_ocr.go).
