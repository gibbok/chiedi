# Dependencies

## modernc.org/sqlite

- Repository: https://gitlab.com/cznic/sqlite
- Version: v1.36.3
- License: BSD-3-Clause
- Purpose: Pure-Go SQLite driver with FTS5 support.
- Why not stdlib/custom: Go's standard library has no SQLite driver; implementing a database is unsafe and out of scope.
- Linking: Static Go dependency; no system SQLite library is required.
- Runtime/network: No runtime service and no network access.
- Security: Database paths are local and queries are parameterized.
- Replacement: The store boundary permits a different SQLite driver later.

## rsc.io/pdf

- Repository: https://github.com/rsc/pdf
- Version: v0.1.1
- License: BSD-3-Clause
- Purpose: Extract text and page provenance from text-layer PDFs.
- Why not stdlib/custom: PDF parsing is complex and security-sensitive; Go has no standard parser.
- Linking: Static Go dependency.
- Runtime/network: No runtime service and no network access.
- Security: Input size/page/text limits are enforced before indexing.
- Replacement: The extractor interface permits replacement after parser benchmarks.
