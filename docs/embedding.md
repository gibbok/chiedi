# Offline multilingual embeddings and installation

Chiedi uses `intfloat/multilingual-e5-small` (MIT), the pinned INT8 ONNX export
at revision `ccc66d3`, and ONNX Runtime 1.23.2. Go calls the native C API
directly through cgo. The Hugging Face tokenizer is linked into the executable
through the daulet/tokenizers 1.27.0 C ABI. No Python, Rust compiler, model
server, cloud account, or network access is needed by an installed application.

## Build and install

Build natively on Linux (glibc) or macOS, on amd64 or arm64. Windows, musl Linux,
cross compilation and `CGO_ENABLED=0` builds are not configured.

Source builds require Go 1.25+, a C/C++ compiler, Python 3, GNU Make and Bash.
On macOS install the Xcode Command Line Tools; on Debian/Ubuntu install
`build-essential python3 make` alongside Go.

```sh
make install
# If necessary, add ~/.local/bin to PATH:
export PATH="$HOME/.local/bin:$PATH"
chiedi version
```

The first build downloads the pinned native archives, tokenizer and approximately
118 MB model weights. Archive checksums are pinned in
`scripts/native-assets.json`. Downloads are cached under `.deps`.
No Rust compiler is required. Model/tokenizer/runtime checksums are recorded in
the installed manifest and verified before loading the model.

`make install` installs the complete application under `~/.local/lib/chiedi`
and links `~/.local/bin/chiedi`. Override the installation root with `PREFIX`.
`make build` prepares the same assets beside `bin/chiedi`.
Keep `assets/` beside the executable when copying a build elsewhere.

`make package` writes a platform archive under `dist/`. Extract it, then run
`bash install.sh "$PWD"` from the extracted directory. The bundle includes
model/tokenizer/runtime license notices. It can also run directly from its
extracted directory. Packaging does not publish a GitHub release.

An explicit `CHIEDI_ASSETS=/absolute/path` overrides asset discovery, mainly for
development. Normal installations need no environment configuration. The
executable resolves symlinks and never loads libraries from the current directory.

## Retrieval behavior

- Query inputs use `query: `; passage inputs use `passage: `, in every language.
- Original query wording reaches E5, including punctuation and stopwords.
  Keyword search independently tokenizes the question and requires all terms,
  so partial matches on grammatical words do not overwhelm cross-language results.
- Each input uses the pinned tokenizer and has at most 512 tokens per native call,
  including its task prefix and special tokens.
- Longer inputs are split into contiguous token windows. Each window retains the
  task prefix; no tail is truncated. Window vectors use mean pooling and L2
  normalization, then combine weighted by content-token count and normalize again.
  This aggregation is Chiedi's policy, not a claim of unlimited model context.
- Output is 384 float32 coordinates. There is no manually maintained synonym map.
- One lazy native session is retained per process, with two intra-op threads and
  serialized inference. All temporary input/output tensors are freed per call.
  The process owns the session until exit; commands that do not embed do not load it.
- Cancellation is checked while waiting for the session and between windows.
  An in-flight native window finishes before cancellation is returned.
- Empty/whitespace input produces a zero vector.

The official INT8 file is named `model_qint8_avx512_vnni.onnx`. It is an ONNX graph,
not an executable instruction set; CPU support and quality still need validation
on each target platform. CI exercises Linux x64 and Apple Silicon. Do not infer
support for an untested CPU from the filename.

## Existing indexes

The old `builtin-semantic-projection-en-v2` vectors are incompatible even though
their dimension is also 384. Chiedi rejects that model identity. Create a new
database, re-add the same roots, and index them using the commands in
[storage and rebuilding](storage.md#rebuilding-and-upgrades). Preserve the old
database until the new one is checked. Unchanged chunks are still reused within
the new model identity.

## Validation and performance

`make verify` and `make verify-full` run the existing dependency, unit, race,
vet, repeated retrieval/indexing, compiled CLI/MCP, PDF/OCR and demo checks.
Tests use the actual bundled model and never download from inside test code.

Embedding coverage includes cross-language queries, relevance against distractors,
normalized finite output, full token-window coverage, long tails, concurrency,
cancellation and corrupt assets. CI also compares native vectors against an
independent Python tokenizer/ONNX mean-pooling reference for five multilingual
query/passage inputs. Reference dependencies are isolated in a CI virtual
environment and are not installed with Chiedi. Benchmarks use the production E5 paths, report
cold initialization separately, and measure English, Italian-to-English and
Czech-to-English retrieval quality. Run:

```sh
make benchmark BENCHMARK_SIZES=20 BENCHMARK_REPEATS=1
CHIEDI_ASSETS="$PWD/bin/assets" go test -run '^$' -bench BenchmarkE5Query ./internal/embedding
```

Model file size is not peak RAM usage. CPU, text length, process startup, PDF
conversion and inference all affect observed performance. MCP amortizes model
loading over many queries in one process.
