# Dependencies

- `modernc.org/sqlite v1.58.0`: pure-Go SQLite driver with FTS5 and bundled `modernc.org/sqlite/vec` (SQLite-vec). Keeps metadata, lexical data and vectors in one transactional database without CGo or an external service. The SQLite-vec code is MIT-licensed, SQLite is public domain, and the Go wrapper is BSD-3-Clause; upstream notices must accompany redistributed source. See https://pkg.go.dev/modernc.org/sqlite/vec.
- `github.com/klippa-app/go-pdfium v1.17.3`: MIT-licensed Go wrapper around PDFium, using its bundled WebAssembly build. Replaces `rsc.io/pdf` for more reliable PDF text decoding, word spacing, and page rasterization. PDFium is BSD-style licensed with third-party notices; preserve the upstream PDFium and wrapper notices when redistributing. No CGo, shared library installation, runtime download, or network service.
- `github.com/tetratelabs/wazero v1.11.0`: Apache-2.0 WebAssembly runtime; directly configured to bound PDFium memory, disable filesystem mounts, and cache compiled engine code locally. Adds a larger binary and first-use compilation cost.
- Optional system `tesseract` (tested with 5.3.4): Apache-2.0 local OCR engine, invoked only for pages requiring OCR or when forced. Language data must be installed separately; the default is English. No OCR executable or trained language data is bundled or downloaded at runtime.

Transitive packages are recorded by `go mod tidy` in `go.mod` and checked by `go.sum`. They support SQLite, PDFium worker pooling, WebAssembly execution, and Unicode decoding. The first build downloads modules; document processing is offline thereafter.

Native embedding dependencies (pinned in `scripts/native-assets.json`):
- `intfloat/multilingual-e5-small`, revision `ccc66d3`, MIT: INT8 ONNX weights and tokenizer. Replaces the curated English projection. About 118 MB of model weights, plus tokenizer/runtime. Tokenizer truncation/padding is disabled at build time; Chiedi supplies complete windows.
- `microsoft/onnxruntime 1.23.2`, MIT with third-party notices: CPU inference through its versioned C API, loaded from the installed asset directory. The C API header is extracted into ignored build output; no additional Go module wrapper is needed.
- `daulet/tokenizers 1.27.0`, Apache-2.0: native static tokenizer library and vendored C ABI declarations in `internal/embedding/tokenizers.h`. Uses Hugging Face tokenizers; preserves the pretrained tokenizer's vocabulary, normalization, and segmentation. Prebuilt static archives avoid a Rust compiler requirement.
- Build setup uses the Go standard library for HTTPS downloads, checksum validation and archive preparation.

Native archive SHA-256 values come from upstream release metadata. Setup records
model/tokenizer/runtime checksums for load-time integrity checks. Bundles carry
the native runtime's license/third-party notices and the model/tokenizer licenses.
See [installation](docs/embedding.md).

The checked-in numerical reference fixture was generated independently with NumPy 2.2.6, ONNX Runtime 1.23.2 and Hugging Face tokenizers 0.22.2. Running the tests requires only Go and the native assets; no Python installation is needed. See `internal/embedding/testdata/README.md` for provenance.
