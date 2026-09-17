# Indexing and chunking

Reconciliation brings the database up to date with registered folders. It avoids
re-extracting unchanged files and re-embedding passages whose embedding input is
unchanged.

[Documentation index](README.md)

## When reconciliation runs

`index` and `reconcile` explicitly refresh every registered root. CLI `search`
and each valid MCP tool call do the same before reading results; CLI `status`
only reads stored state. There is no background watcher or timer, so a disk edit
reaches the index on the next refresh.

Each refresh still walks the folders and checks metadata. An unchanged corpus
avoids extraction and document embedding, but the walk takes time as the number
of files grows. A search path filter narrows results, not this refresh work.

## File discovery and extraction

- Recursively scan registered roots for `.txt`, `.md`, and `.pdf`, case-insensitively.
- Index only regular files; skip symlinks, pipes, sockets, and devices. Root
  registration resolves the root's symlinks first.
- There is no `.gitignore` integration or configurable exclusion pattern:
  supported files in hidden directories are eligible too. Register roots deliberately.
- The **64 MiB source-file limit applies to all three formats**, including TXT
  and Markdown, not just PDFs.
- Registering the same canonical root twice has no effect. Overlapping roots
  (a parent and its subdirectory) index shared files separately under each root;
  there is no cross-root document deduplication.
- TXT and Markdown require UTF-8 and normalize line endings/trailing whitespace.
- Markdown recognizes ATX headings (`#` through `######` followed by a space),
  with hierarchy such as `Guide > Setup`; fenced blocks stay in section text.
  This is a lightweight parser, not a full Markdown renderer.
- TXT/PDF titles come from the filename; Markdown uses its first H1 when available.
- Empty TXT/Markdown files can be indexed with zero chunks; PDFs without readable
  sections fail. Each readable PDF page retains its original number. See
  [PDF conversion](pdf-conversion.md).

## Incremental reconciliation algorithm

For each root, load stored documents and match discovered files by relative path,
then by device/inode identity where the platform exposes it.

1. **Metadata fast path:** if size, nanosecond mtime, available identity, and format
   match, skip reading and extraction. This trusts metadata; an in-place edit that
   preserves all those values is not detected by this fast path.
2. **Rename reuse:** a same-root identity match can update the path and FTS path
   without extraction or embedding when metadata and format match. Cross-root
   moves and filesystems without identity are not guaranteed this reuse.
3. **Content check:** read within the file-size bound and hash bytes with SHA-256.
   If the same-format content hash matches, update metadata only.
4. **Extraction:** parse changed content, then recheck size, mtime, and identity.
   A file observed changing during extraction is deferred with an error.
5. **Chunk reuse:** hash each new embedding input and look for an old valid vector
   with that hash in the same document. Consume matches individually; embed only
   unmatched inputs. There is no cross-document embedding cache.
6. **Commit and cleanup:** replace a document atomically; delete previously stored
   documents absent from a completed root scan. Traversal errors defer that root's
   stale-document deletion; unavailable roots retain their indexed data.

A pure rename retains existing chunks and embeddings, including any filename
context used when they were originally embedded. Re-extraction can change that
context. Reused vectors do not imply reused chunk IDs on document replacement.

For example, editing one Markdown section re-extracts that file, but only chunks
whose text or title/heading context changed need new vectors. Changing a title
can affect every chunk; adding a paragraph can change nearby chunk boundaries.
Changing only the file's timestamp reads and hashes it, then skips extraction
if its bytes are unchanged.

Successful document updates remain committed if another file or root fails.
CLI search and MCP stop on a reconciliation error rather than serving results
from that partial refresh. Stored extraction failures are different: they remove
that document's searchable chunks but do not by themselves fail the whole run.

## Chunking algorithm

Chunks keep enough context for retrieval while bounding individual passages.
Lengths are Unicode runes, not bytes or model tokens.

- Process each extracted section separately; chunks do not cross Markdown sections
  or PDF pages.
- Split on blank paragraphs and accumulate toward **1,800 runes**. Flush before
  adding a piece that would exceed that target.
- Hard-split paragraphs longer than **3,000 runes** into windows with **200-rune
  overlap**. Overlap applies only to these oversized paragraphs, not every chunk.
- A single piece can exceed the 1,800-rune target, up to the 3,000-rune hard limit.
- Preserve ordinal, heading, page start/end, and the source relationship.
- Prefix the chunk body with document title and section heading, then hash the
  entire embedding input with SHA-256. Changing title or heading can invalidate
  reuse even if the body stays the same:

```text
Document: <title>
Section: <heading>

<chunk text>
```

## Failures and work statistics

- Source-size violations and extraction errors store a failed document with its
  reason; replacement removes any previously indexed chunks for that document.
- Filesystem/read/scan errors appear in run `errors` and can make reconciliation
  return an error. They are distinct from stored failed-document records.
- Unchanged failed files are skipped too; installing OCR or changing configuration
  alone does not retry them. Use a fresh index to reprocess unchanged content.
- `failed_documents` and `failed_document_paths` describe failures in this run.
  Stored failures remain visible in [status](storage.md#diagnosing-failures).
- `scanned_files` counts supported-extension entries encountered, so it can include
  an entry later rejected as nonregular. `embedding_jobs` counts new chunk vectors;
  `reused_chunks` counts reused vectors during re-extraction, not every skipped chunk.
- The run also reports `extraction_jobs`, `renamed_documents`, `deleted_documents`,
  and `completed_at`; the last completed summary is persisted.

Implementation: [indexer](../internal/indexer/indexer.go),
[extraction](../internal/extract/extract.go), [chunking](../internal/chunk/chunk.go).
