# Retrieval algorithms

Hybrid retrieval combines two useful signals: words that match the question and
passages with similar learned embeddings. It returns ranked source chunks, not a
generated answer or a calibrated confidence score.

[Documentation index](README.md)

## Query to evidence

```mermaid
flowchart TD
    Query["Prepared question"] --> Embed["Local E5 inference"]
    Query --> FTS["FTS5 / BM25"]
    Embed --> Vector["Exact cosine search"]
    FTS --> Fusion["Reciprocal rank fusion"]
    Vector --> Fusion
    Fusion --> Evidence["Ranked source chunks"]
```

- CLI search and MCP tools reconcile roots first. `Retriever.Retrieve` itself
  does not scan files; internal callers control refresh explicitly.
- Trim surrounding query whitespace and embed the original question. Lexical
  search independently lowercases and tokenizes the query into Unicode words.
- Read up to **40 vector candidates** and **40 lexical candidates** from one SQLite
  read snapshot. Their union can contain up to 80 chunks.
- Fuse ranks and return the requested number of chunks: default 10, maximum 50.
  Several chunks can come from one document; production retrieval does not deduplicate documents.

## Multilingual E5 embeddings

The production model is `intfloat/multilingual-e5-small`, pinned to revision
`ccc66d3` with INT8 weights and 384 float32 output coordinates. Inference runs
locally through ONNX Runtime's C API and the bundled native tokenizer.

Questions retain their original wording and receive `query: `; document chunks
receive `passage: `. Each window has at most 512 tokens including prefix and
special tokens. Mean pooling includes every unpadded token; vectors are
L2-normalized. Longer text is covered by contiguous windows and their normalized
vectors are averaged with content-token-count weights, then normalized again.
No source text is truncated or rewritten. See [embedding and installation](embedding.md).

There is no curated synonym dictionary, suffix stripping or English stopword
filter. Empty/whitespace input gives a zero vector. Learned similarity depends
on language, context and domain; it is not a calibrated confidence score.

## Lexical ranking

FTS5 provides exact-term matching over chunk text, heading, and relative path.

- Build quoted, distinct query terms of at least two runes, joined with `AND`.
- Require all query terms for the lexical channel. This prevents a few shared
  grammatical words from boosting an unrelated passage above a cross-language
  semantic match. Natural-language questions can rely on E5 when the complete
  wording is absent; exact identifiers and keyword conjunctions retain FTS support.
- If none remain, use a sentinel term (`__no_match__`).
- Order matches by SQLite `bm25(chunks_fts)` ascending, with default column weights.
- User input is converted into terms; it is not passed through as an advanced FTS
  expression. Phrase/proximity syntax is not exposed by the CLI or MCP tool.

## Vector ranking

SQLite-vec's `vec0` table performs exact nearest-neighbor search by cosine distance.
For nonzero vectors, cosine similarity is `dot(q, v) / (norm(q) * norm(v))`;
cosine distance is `1 - similarity`.

- Validate query dimensions and reject non-finite coordinates.
- Apply a literal path-prefix filter before choosing nearest neighbors.
- Request the closest 40 rows, then discard null/NaN distances and distances
  **greater than or equal to 1** (nonpositive similarity). There is no refill.
- Record `vector_score = 1 - distance` and rank the retained vector candidates.
- Zero vectors in the index can yield unusable distances and are excluded.

This is an exact scan, not an approximate nearest-neighbor graph. Distance work
grows with the eligible chunk count and vector dimension; fetching a bounded
candidate union does not make the underlying scan constant-time.

## Reciprocal rank fusion

For each chunk, add a contribution from every ranking in which it appears:

```text
score(chunk) = vector contribution + lexical contribution
contribution = 1 / (60 + rank), or 0 when absent from that ranking
```

- Ranks start at 1. Both channels have equal weight; raw BM25 and cosine values
  are not added together.
- Example: vector rank 2 and lexical rank 1 gives `1/62 + 1/61 ≈ 0.03252`.
  A chunk present only at vector rank 1 gets `1/61 ≈ 0.01639`.
- Sort by fused score descending, then vector score descending, then chunk ID
  ascending. SQL rank ties have no explicit secondary ordering, so the final
  tie-break does not guarantee identical rankings across all environments.
- Results include `score`, optional `vector_score`/`lexical_rank`, rank, full chunk
  text, chunk/document IDs, root/path, and available heading/page references.

There is no learned reranker, relevance threshold on the fused score, or query
answering step. Evaluate known questions with [retrieval accuracy metrics](benchmarks.md)
and inspect their sources before treating a match as useful evidence.

Implementation: [query preparation and fusion](../internal/retrieval/retrieval.go),
[E5 embedding](../internal/embedding/embedding.go),
[vector candidates and snapshots](../internal/store/vectors.go),
[FTS query](../internal/store/store.go).
