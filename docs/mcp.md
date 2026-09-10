# MCP integration

The MCP server lets a client retrieve passages, expand their context, and inspect
indexed sources. It exposes the same local index used by the CLI.

[Documentation index](README.md)

## Launch and transport

Configure your client's stdio server entry with:

- Name: `chiedi`.
- Executable: `/absolute/path/to/bin/chiedi`.
- Arguments: `["mcp"]`.
- Environment: `CHIEDI_DB=/absolute/path/to/index.db`, plus any OCR settings.

The equivalent process command is:

```sh
CHIEDI_DB=/absolute/path/to/index.db /absolute/path/to/bin/chiedi mcp
```

- Newline-delimited JSON-RPC 2.0 on stdin/stdout; no HTTP endpoint or
  `Content-Length` framing.
- Initialization advertises protocol version `2025-06-18`, server `chiedi` version
  `0.1.0`, and tools capability with `listChanged: false`.
- Handles `initialize`, `ping`, `tools/list`, and `tools/call`; notifications
  without an ID receive no response.
- Requests are processed sequentially, with a 4 MiB scanner limit per input line.
- Arguments reject unknown fields, `null`, and trailing JSON values. Tool discovery
  provides the schemas; host configuration syntax depends on the client.

## Tools

Every supported tool reconciles first, including reads and status. A reconciliation
error fails the call; requests do not silently fall back to stale evidence.

| Tool | Arguments | Result |
| --- | --- | --- |
| `retrieve` | Required `question`; optional `limit` (1–50, default 10), `path_prefix` | Ranked chunks with scores and source provenance |
| `read_chunks` | Required `chunk_ids` (1–100 positive IDs); `before`/`after` (0–5 each, default 0) | Selected chunks and deduplicated neighbors within each document |
| `list_documents` | Optional `path_prefix`, `extension`, `status` (`indexed` or `failed`) | Document metadata and available failure reasons |
| `index_status` | Empty object | Database/model metadata, counts, current failure paths, and last reconciliation |

- Prefixes are literal, case-sensitive root-relative prefixes across all roots,
  with backslashes normalized to `/`. They are not root selectors.
- Extension matching is case-insensitive and accepts `pdf` or `.pdf`.
- `list_documents` has no pagination or limit parameter.
- The retrieve implementation also treats `limit: 0` as default, but its advertised
  schema permits 1–50; clients should omit the field for the default.

## Result shape and evidence identity

Collection tools (`retrieve`, `read_chunks`, `list_documents`) return an object
with a `results` array, including `[]` for no results. `index_status` returns a
status object directly. Both appear in `structuredContent` and as serialized JSON
in a text content item; successful calls set `isError: false`.

- Use `root_id`, `root_path`, and root-relative `path` together to identify sources.
- Heading and page fields appear when available; PDF pages retain original numbering.
- Retrieval returns chunks, while `read_chunks` adds ordinal information and nearby
  passages without scores. Neighbors stay within the same document, but may cross
  its section/page boundaries.
- Chunk IDs are database-local references. After a document replacement or deletion,
  an old ID can be stale; `read_chunks` fails with “retrieve again” rather than
  returning a different passage under that ID. Obtain new IDs with `retrieve`.

Example raw request (one line on stdin):

```json
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"retrieve","arguments":{"question":"deployment checklist","limit":5}}}
```

Use actual returned IDs for the next `read_chunks` call. Keep source references
alongside text when composing an answer.

## Errors and privacy boundary

- Tool/argument/indexing failures return a tool result with `isError: true` and
  error text. Invalid protocol requests use JSON-RPC error codes such as `-32700`,
  `-32600`, `-32601`, and `-32602`.
- Chiedi itself processes documents locally. The MCP client receives text and paths;
  whether it forwards them to a remote model depends on the client.
- Test the local protocol with [the verification suite](testing.md). A real client
  connection remains a separate manual smoke check.

Implementation: [MCP server and schemas](../internal/mcp/server.go),
[evidence reads](../internal/store/store.go), [retrieval results](../internal/retrieval/retrieval.go).
