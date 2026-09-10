# PDF conversion

Chiedi extracts PDF text one page at a time and can read scans with local OCR.
Returned passages retain the original PDF page numbers, including gaps for blank pages.

[Documentation index](README.md)

## Text and scanned pages

- Bundled PDFium runs through `go-pdfium` and wazero WebAssembly; no native PDFium
  installation or runtime download is needed.
- Each readable page becomes a separate section. PDF headings remain in the text;
  they are not inferred as Markdown heading hierarchies.
- In default `auto` mode, use a text layer containing at least one letter or number;
  otherwise inspect/render the page and use Tesseract if it is nonblank.
- Pages with neither objects nor annotations, or whose raster is entirely white/transparent,
  are skipped. The raster check has no tolerance: faint marks and noisy blank scans
  may still require OCR.
- A page conversion/OCR error fails the whole document with a page-specific reason;
  other documents can still be indexed. PDFs with no readable sections fail too.

## OCR setup

Tesseract and its language data are optional system dependencies for scans.

```sh
# macOS
brew install tesseract

# Debian / Ubuntu: English plus Czech
sudo apt-get install tesseract-ocr tesseract-ocr-eng tesseract-ocr-ces

tesseract --list-langs
```

| Variable | Values | Behavior |
| --- | --- | --- |
| `CHIEDI_PDF_OCR` | `auto` (default) | Use readable text layers; OCR nonblank pages without usable text |
| `CHIEDI_PDF_OCR` | `always` | Bypass text decoding and OCR visually nonempty pages |
| `CHIEDI_PDF_OCR` | `off` | Disable external OCR; fail a nonblank page requiring it |
| `CHIEDI_OCR_LANG` | `eng` (default), or installed names joined with `+` | Select Tesseract language data |

```sh
./bin/chiedi --db scans.db add /absolute/path/to/documents
CHIEDI_OCR_LANG=ces+eng ./bin/chiedi --db scans.db index
```

Set these variables on the MCP process too. Changing settings or installing OCR
alone does not reprocess unchanged indexed **or failed** files. Follow the
[fresh-index procedure](storage.md#rebuilding-and-upgrades) to apply new settings.

## Page algorithm and limits

- `auto`/`off` try text decoding first; `always` skips it. A text decoding error
  fails extraction in `auto`/`off` rather than falling through to OCR.
- A nonblank page needing OCR is rendered within **2,400 × 3,200 pixels**, including
  annotations via `FPDF_RENDER_FLAG_ANNOT`.
- Encode the raster as PNG in memory and invoke `tesseract stdin stdout -l <language>`
  without a shell. Document images pass through stdin, not temporary source copies.
- Bound Tesseract to **two minutes per page**, **4 MiB stdout**, and **16 KiB stderr**.
  Output must contain a letter or number.
- Source limit: **64 MiB**; PDF limit: **2,000 pages**, **128 MiB extracted text**,
  and **1,048,576 decoded text characters per page** (up to 4 MiB UTF-8).
- PDFium has **512 MiB WASM memory**, no filesystem mounts, and at most one worker
  per process. Compiled engine code is cached in the OS user cache under
  `chiedi/pdfium`; failure to create a cache does not block conversion.
- Cancellation is checked between PDF operations and when waiting for a worker;
  Tesseract is terminated on cancellation. An active PDFium call is not interruptible
  through the wrapper's current API.

## Limitations

- Reading order follows PDFium extraction; tables and columns are not converted
  into structured Markdown tables.
- Automatic mode does not detect every broken text layer or scanned region next
  to valid text. Use `always` where OCR of the whole page is needed.
- OCR can misread text. Missing language data, unreadable nonblank images, and PDFs
  requiring a password fail; there is no password parameter.
- An image-only diagram with no recognizable text can fail conservatively.
- Annotation-only pages are considered for OCR; automatic mode may skip annotation
  text when a readable text layer is already present.

## Verification

Fixtures are generated in temporary directories. Enable real English Tesseract
acceptance tests with:

```sh
CHIEDI_TEST_OCR=1 go test -v -count=1 ./internal/extract
CHIEDI_TEST_OCR=1 make test-e2e
```

The full [verification gates](testing.md) also cover mixed digital/scanned PDFs,
blank-page numbering, changed OCR text, malformed inputs, and embedding reuse.
For the optional external fixture, `CHIEDI_TEST_PUBLIC_PDF` must point to the W3C
[Dummy PDF sample](https://www.w3.org/WAI/ER/tests/xhtml/testfiles/resources/pdf/dummy.pdf)
on disk; the test does not download it. Set `CHIEDI_TEST_OCR=1` and run
`go test -v -count=1 ./internal/e2e -run TestBuiltBinaryPDFConversion`.
Keep exploratory PDFs in ignored `temp/`, not in commits.

Implementation: [PDF extraction](../internal/extract/pdf.go),
[OCR and bounds](../internal/extract/pdf_ocr.go),
[PDF tests](../internal/extract/pdf_test.go), [binary PDF tests](../internal/e2e/pdf_test.go).
