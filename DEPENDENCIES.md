# Dependencies

- `modernc.org/sqlite v1.58.0`: pure-Go SQLite driver with FTS5 and bundled `modernc.org/sqlite/vec` (SQLite-vec). Keeps metadata, lexical data and vectors in one transactional database without CGo or an external service. The SQLite-vec code is MIT-licensed, SQLite is public domain, and the Go wrapper is BSD-3-Clause; upstream notices must accompany redistributed source. See https://pkg.go.dev/modernc.org/sqlite/vec.
- `github.com/klippa-app/go-pdfium v1.17.3`: MIT-licensed Go wrapper around PDFium, using its bundled WebAssembly build. Replaces `rsc.io/pdf` for more reliable PDF text decoding, word spacing, and page rasterization. PDFium is BSD-style licensed with third-party notices; preserve the upstream PDFium and wrapper notices when redistributing. No CGo, shared library installation, runtime download, or network service.
- `github.com/tetratelabs/wazero v1.11.0`: Apache-2.0 WebAssembly runtime; directly configured to bound PDFium memory, disable filesystem mounts, and cache compiled engine code locally. Adds a larger binary and first-use compilation cost.
- Optional system `tesseract` (tested with 5.3.4): Apache-2.0 local OCR engine, invoked only for pages requiring OCR or when forced. Language data must be installed separately; the default is English. No OCR executable or trained language data is bundled or downloaded at runtime.

Transitive packages are recorded by `go mod tidy` in `go.mod` and checked by `go.sum`. They support SQLite, PDFium worker pooling, WebAssembly execution, and Unicode decoding. The first build downloads modules; document processing is offline thereafter.

The built-in embedding implementation uses the standard library and a curated English synonym dictionary. It does not download or bundle trained model weights.
