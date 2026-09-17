# Developer documentation

Chiedi indexes local files and returns cited passages through a CLI and an MCP
server. Start with a small folder, then use the guides below for implementation
and operational details.

| Guide | What you will learn |
| --- | --- |
| [Getting started and CLI](getting-started.md) | Build, select a database, manage roots, search, and configure the process |
| [Architecture](architecture.md) | Runtime boundaries, package responsibilities, and extension points |
| [Indexing and chunking](indexing.md) | Change detection, extraction, provenance, and embedding reuse |
| [Embeddings and installation](embedding.md) | How text becomes vectors, local model inference, and native bundles |
| [Retrieval algorithms](retrieval.md) | BM25 candidates, cosine search, and reciprocal rank fusion |
| [PDF conversion](pdf-conversion.md) | Text extraction, scanned pages, OCR settings, and limits |
| [MCP integration](mcp.md) | Transport, tool contracts, result shapes, and stale references |
| [Storage and operations](storage.md) | Schema, transactions, failure diagnosis, privacy, and rebuilding |
| [Development and testing](testing.md) | Verification gates, source dependencies, and contribution workflow |
| [Benchmarks](benchmarks.md) | Latency measurements, accuracy metrics, and reproducibility |

## Scope at a glance

- Supported sources: regular `.txt`, `.md`, and `.pdf` files in registered folders.
- Processing: local extraction and embeddings; no application telemetry or runtime network service.
- Retrieval: ranked chunks, with root/path and heading or page provenance where available.
- Refresh: on-demand reconciliation before CLI search and every supported MCP tool; no background watcher.
- Scale: exact vector scanning and filesystem walks; use the benchmark on representative documents.
- Semantic coverage: bundled multilingual E5-small INT8, with quality dependent on language and domain.
- Installation: [native model/runtime bundle](embedding.md), with no runtime downloads.

Each guide links to its implementation sources. Defaults and limits describe the
current code; proposed extensions are identified explicitly.
