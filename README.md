# chiedi

**Find the evidence in your documents. Bring it into your developer workflow.**

`chiedi` makes local Markdown, text files, and PDFs searchable from your terminal
and available to Codex or other AI Agent Harnesses through MCP, with source paths, headings, and PDF page references.

Built in Go, with SQLite FTS5, exact vector search, and bundled multilingual E5 embeddings.

## What can you use it for?

- **Find project decisions:** locate a migration note, design rationale, or deployment checklist across your Markdown documentation.
- **Look up a reference:** search for a release identifier or a phrase buried in a PDF manual.
- **Give your harness source material:** retrieve relevant passages and nearby context for answers you can check against the originals.
- **Keep a working archive searchable:** pick your folders; searches reconcile additions, edits, renames, and deletions automatically.

## Why chiedi?

- **Local processing:** no model server, cloud account, or document upload is required by chiedi.
- **Evidence you can inspect:** results contain the original passage and its source location.
- **Search beyond identical wording:** keyword matching works alongside a trained multilingual embedding model.
- **A small setup:** one installation containing the executable, model, tokenizer, and native runtime, with optional Tesseract for scanned PDFs.

Chiedi retrieves evidence; the connected assistant decides how to use it. Its
embeddings use intfloat/multilingual-e5-small locally through ONNX Runtime's C API.
Retrieval quality varies by language and document. Chiedi processes documents
locally; an MCP client receives retrieved text and controls whether it sends that
text to a remote model.

## Get started

Build and install with `make install` (Go, a C/C++ compiler, Make, and Bash).
The first build downloads pinned assets; installed processing is fully offline.
See [native installation](docs/embedding.md) for platform requirements and bundles.

Follow the [quick start](docs/getting-started.md) to build, index a folder, and run
your first search. Then [connect an MCP client](docs/mcp.md).

For a hands-on walkthrough, run `make demo` from a checkout with Go installed.

- [Developer documentation](docs/README.md)
- [Architecture](docs/architecture.md)
- [Retrieval algorithms](docs/retrieval.md)
- [Development and testing](docs/testing.md)
