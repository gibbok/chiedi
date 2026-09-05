# PDF conversion

PDFs are decoded with PDFium, the specialized engine also used by
[`conductor-oss/markitdown`](https://github.com/conductor-oss/markitdown).
We use `go-pdfium` directly because MarkItDown's public PDF converter combines
pages into one string and can suppress individual page extraction errors.
Direct integration keeps original page numbers and reports conversion failures.
It also avoids importing converters for unrelated formats.

PDFium supplies word spacing, Unicode decoding, and reading order. Each original
page becomes a separate section, so a blank second page does not turn page 3 into
page 2. Source path and page citations continue through chunking and retrieval.
PDF headings are retained in page text, rather than inferred as Markdown heading
hierarchies. UTF-8 `.txt` and `.md` handling is unchanged; DOCX and other new
extensions are not added in this change.

## Scanned PDFs

The default `DOCDEX_PDF_OCR=auto` extracts text first and runs local Tesseract
when a nonblank page has no letters or numbers. It handles image-only documents
and mixed documents containing digital and scanned pages. Empty pages and pages
whose rendering is entirely white/transparent are skipped, including white
graphics and blank scan images, without requiring Tesseract. This check uses no
color tolerance, so faint marks are not treated as blank; noisy blank scans may
still require OCR. Missing OCR tools, missing language data, or unreadable nonblank pages
fail the document with a page-specific error instead of silently indexing only
part of it. Other documents remain indexable. OCR rendering includes visible
non-interactive annotations such as approval stamps; annotation-only pages are
not treated as empty. Interactive form widgets and popup comments are not
included by this rendering flag. As with images beside valid digital text, use
`always` when a stamp must be read on a page that already has a usable text layer.

Install Tesseract before indexing scans:

```sh
# macOS
brew install tesseract

# Debian / Ubuntu (English plus Czech, if needed)
sudo apt-get install tesseract-ocr tesseract-ocr-eng tesseract-ocr-ces
```

Use `tesseract --list-langs` to inspect installed languages. For Czech and English:

```sh
DOCDEX_OCR_LANG=ces+eng ./bin/docdex --db fresh.db index
```

Set these environment variables on the MCP process as well when it performs
indexing or reconciliation.

| Variable | Values | Behavior |
| --- | --- | --- |
| `DOCDEX_PDF_OCR` | `auto` (default) | OCR pages without usable text. |
| `DOCDEX_PDF_OCR` | `always` | Bypass text decoding and OCR every visually nonempty page; useful for broken text layers or scanned text alongside a digital header. |
| `DOCDEX_PDF_OCR` | `off` | No external OCR; a page requiring OCR reports an error. |
| `DOCDEX_OCR_LANG` | `eng` (default), or installed Tesseract language names joined with `+` | Language data used for OCR. |

OCR stays on the machine. Rasterized pages are passed through memory/stdin;
there are no temporary document copies or cloud calls. Compiled PDFium engine
code is cached under the OS user cache directory in `docdex/pdfium`; that cache
contains no document data. If the cache cannot be created, compilation proceeds
in memory.

## Existing indexes and limitations

Unchanged files keep their existing chunks. To apply the new conversion engine
to an old corpus, create a new database, add the same roots, and index again:

```sh
./bin/docdex --db improved.db init
./bin/docdex --db improved.db add /absolute/path/to/documents
./bin/docdex --db improved.db index
```

Use that new database for search and MCP after checking the results. The same
applies after changing OCR settings or installing missing language data: changing
configuration alone does not invalidate already indexed or failed files.

- PDF text reading order depends on the source. Complex tables and columns are
  not guaranteed to become structured Markdown tables.
- Automatic mode cannot detect every incorrect text layer or image region beside
  valid text. Use `always` for those documents. OCR can misread text, especially
  handwriting, low-resolution scans, or text in a language not configured.
- Password-protected PDFs requiring a password fail; no password interface is
  introduced. A nonblank image/diagram page with no recognizable text also fails
  conservatively rather than being silently omitted.
- Limits: 64 MiB source, 2,000 pages, 128 MiB total text, and at most 1,048,576
  PDF text characters per page (4 MiB worst-case UTF-8). OCR output is limited to
  4 MiB per page. PDFium has a 512 MiB WASM memory limit and one active worker per
  process. OCR images fit within 2,400 × 3,200 pixels, with a two-minute Tesseract
  timeout per page.
- Cancellation is checked between PDF operations and while waiting for a worker;
  Tesseract is terminated on cancellation. An in-progress PDFium operation cannot
  be interrupted by the wrapper's current API.

## Verification

PDF test documents are generated at runtime in `t.TempDir()`. Keep exploratory
documents and results in the ignored repository `temp/` directory; never commit
them. The focused acceptance test requires real Tesseract and English data:

```sh
DOCDEX_TEST_OCR=1 go test -v -count=1 ./internal/extract
DOCDEX_TEST_OCR=1 make verify
DOCDEX_TEST_OCR=1 make verify-full
```

Without `DOCDEX_TEST_OCR=1`, only the real-Tesseract acceptance case is skipped;
PDF decoding, page provenance, missing OCR, malformed files, limits, and
cancellation regressions still run. The complete gates retain all existing CLI,
MCP, indexing, SQLite-vec, retrieval, race, vet, and reconciliation checks.
CI installs Tesseract and requires the real OCR acceptance case in both gates.

The compiled-binary PDF E2E test generates valid digital PDFs and mixed PDFs
with actual JPEG scan images in temporary directories. It exercises CLI indexing
and search, MCP retrieval and `read_chunks`, original page-3 citations after a
blank page, malformed-document isolation, and a changed scan whose new OCR text
is indexed while the unchanged digital cover reuses its embedding. It also
checks no work is repeated on unchanged files and runs the database health check.

To include the independently authored W3C PDF test sample in that same E2E run:

```sh
mkdir -p temp/public-pdfs
curl -fL https://www.w3.org/WAI/ER/tests/xhtml/testfiles/resources/pdf/dummy.pdf \
  -o temp/public-pdfs/w3c-dummy.pdf
DOCDEX_TEST_OCR=1 \
DOCDEX_TEST_PUBLIC_PDF="$PWD/temp/public-pdfs/w3c-dummy.pdf" \
  go test -v -count=1 ./internal/e2e -run TestBuiltBinaryPDFConversion
```

`DOCDEX_TEST_PUBLIC_PDF` specifically expects that sample (containing “Dummy PDF
file”). The E2E test copies it to its temporary corpus and verifies retrieval.
Only the explicit `curl` command accesses the network; tests and application
processing remain offline, and CI uses generated PDFs without requiring W3C
availability. Neither downloaded nor generated PDF files should be committed.
