# Storage and operations

The SQLite index holds extracted text, source metadata, lexical data, and vectors.
It can be rebuilt from registered source folders; keeping those originals is essential.

[Documentation index](README.md)

## Data model and consistency

| Object | Contents |
| --- | --- |
| `meta` | Schema version, embedding identity/dimensions, vector dimensions, last reconciliation |
| `roots` | Canonical absolute directories |
| `documents` | Root-relative paths, identity/size/mtime/hash, format, status, error |
| `chunks` | Ordinals, headings/pages, text, embedding-input hash, float32 vector blob |
| `chunks_fts` | FTS5 text, heading, path; row ID matches the chunk |
| `chunks_vec` | `vec0` cosine vectors; row ID matches the chunk |

- Current schema version: **2**. The vector table is created lazily with fixed
  dimensions on the first vector write; production embeddings use 384 dimensions.
- Document replacement, rename, deletion, and root removal maintain their relevant
  relational/FTS/vector data transactionally. The whole reconciliation is not one transaction.
- Chunk IDs use `AUTOINCREMENT` and are not reused after deletion. Document
  replacements allocate new chunk IDs even when their vectors can be reused.
- The stored vector exists both as a little-endian float32 blob on `chunks` and
  in `chunks_vec`; `doctor` checks their agreement.
- SQLite uses WAL, `synchronous=NORMAL`, foreign keys, a 5-second busy timeout, and
  one database connection per `Store` instance. Separate CLI/MCP processes can
  access the same file; write paths acquire a lock before reading data they modify.
- Lexical ranks, vector ranks, and candidate text share a read snapshot.
  `read_chunks` has its own snapshot. Neither guarantees the filesystem stays
  unchanged between separate calls.

## Local data and privacy

- The database is created/chmodded to `0600`; newly created parent directories use
  `0700`. Existing directory permissions are not rewritten.
- WAL can create `-wal` and `-shm` sidecars beside the database. Treat the index,
  sidecars, backups, and exported results as sensitive document data.
- The database is not encrypted by the application. Use an appropriately protected
  local directory and operating-system storage controls.
- Chiedi has no telemetry or document-processing network calls. Initial module or
  OCR installation can need network access; the MCP host controls onward use of evidence.
- PDFium caches compiled engine code under the OS user cache in `chiedi/pdfium`,
  without source document data. OCR page images pass through memory/stdin.

## Diagnosing failures

Start with the stored view, then explicitly refresh if needed:

```sh
./bin/chiedi status
./bin/chiedi reconcile
./bin/chiedi doctor
```

- `status.counts.failed_count` is the number of currently stored failures;
  top-level `failed_document_paths` contains their absolute paths, ordered by root
  and relative filename. Counts and paths come from one snapshot.
- `last_reconciliation.failed_documents` and its paths describe only that run.
  An unchanged run can report zero while stored failures remain.
- MCP `list_documents` with `{"status":"failed"}` exposes per-file reasons.
- CLI `status` does not scan. MCP `index_status` scans first and can fail on an
  unavailable root. Use CLI status to inspect stored state in that situation.
- `doctor` checks database/index consistency and model compatibility; it does not
  repair corruption or verify source freshness.

| Symptom | Next step |
| --- | --- |
| Scan requires OCR / missing language | Install Tesseract/data, set OCR options, then rebuild to retry unchanged files |
| No results or zero roots | Check `--db`, `CHIEDI_DB`, and `roots`; confirm supported files are present |
| Root unavailable / scan incomplete | Restore access and reconcile again; stale deletion is deferred for incomplete traversal |
| Stale chunk ID | Retrieve again, then use the new IDs |
| Embedding mismatch / index corruption | Preserve the old database and create a fresh index from sources |

## Rebuilding and upgrades

Schema-1 indexes migrate to schema 2 transactionally without re-embedding. Newer
unsupported schemas are rejected. Model identity and dimension mismatches are
rejected rather than mixing incompatible vectors.

Changes to OCR settings, installed languages, or extraction behavior do not
invalidate unchanged files. There is no force-reindex command. For a full refresh:

```sh
./bin/chiedi --db rebuilt.db init
./bin/chiedi --db rebuilt.db add /absolute/path/to/documents
./bin/chiedi --db rebuilt.db index
./bin/chiedi --db rebuilt.db doctor
./bin/chiedi --db rebuilt.db search "known reference"
```

Repeat `add` for each root, check counts/failures and representative searches, then
point CLI and MCP at the new database. Keep the old index until verified. For a
file-level backup, stop all writers and close processes before copying; do not
copy only a live main database while uncheckpointed WAL data may exist.

Implementation: [schema and mutations](../internal/store/store.go),
[vector migration/snapshots](../internal/store/vectors.go),
[status snapshot](../internal/store/status.go), [compatibility](../internal/indexer/indexer.go).
