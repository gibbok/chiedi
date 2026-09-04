# Development rules

- Keep the application local-first: no telemetry, cloud calls, or runtime service.
- Prefer the Go standard library. Document every dependency in `DEPENDENCIES.md`.
- Preserve path, heading, and PDF page provenance for every indexed chunk.
- Store metadata, full-text data, and vectors transactionally in one SQLite file.
- Never re-embed unchanged chunks. Filesystem events are hints; reconciliation is authoritative.
- Add tests for behavior changes. Do not disable or weaken failing tests.
- Completion requires `make verify` and `make verify-full` to pass.
