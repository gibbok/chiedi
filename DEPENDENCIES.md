# Dependencies

- `modernc.org/sqlite v1.58.0`: pure-Go SQLite driver with FTS5 and bundled `modernc.org/sqlite/vec` (SQLite-vec). Keeps metadata, lexical data and vectors in one transactional database without CGo or an external service. The SQLite-vec code is MIT-licensed, SQLite is public domain, and the Go wrapper is BSD-3-Clause; upstream notices must accompany redistributed source. See https://pkg.go.dev/modernc.org/sqlite/vec.
- `rsc.io/pdf v0.1.1`: BSD-licensed text-layer PDF parser. No OCR or external command dependency.

Transitive packages are recorded by `go mod tidy` in `go.mod` and checked by `go.sum`. They support the pure-Go SQLite runtime and its platform abstractions. The first build downloads modules; document processing is offline thereafter.

The built-in embedding implementation uses the standard library and a curated English synonym dictionary. It does not download or bundle trained model weights.
