#!/usr/bin/env bash
set -euo pipefail

DOCDEX_BIN="${1:-./bin/docdex}"

if [[ ! -x "$DOCDEX_BIN" ]]; then
  printf 'error: executable not found: %s\n' "$DOCDEX_BIN" >&2
  printf 'run `make build` first, or use `make demo`\n' >&2
  exit 1
fi

CREATED_TEMP=0
if [[ -n "${DOCDEX_DEMO_DIR:-}" ]]; then
  DEMO_DIR="$DOCDEX_DEMO_DIR"
  if [[ -d "$DEMO_DIR" && -n "$(find "$DEMO_DIR" -mindepth 1 -print -quit)" ]]; then
    printf 'error: DOCDEX_DEMO_DIR must be empty: %s\n' "$DEMO_DIR" >&2
    exit 1
  fi
  mkdir -p "$DEMO_DIR"
else
  DEMO_DIR="$(mktemp -d "${TMPDIR:-/tmp}/docdex-demo.XXXXXX")"
  CREATED_TEMP=1
fi

cleanup() {
  if [[ "$CREATED_TEMP" == 1 && "${KEEP_DEMO:-0}" != 1 ]]; then
    rm -rf "$DEMO_DIR"
  else
    printf '\nDemo files preserved in %s\n' "$DEMO_DIR"
  fi
}
trap cleanup EXIT

CORPUS="$DEMO_DIR/documents"
DB="$DEMO_DIR/docdex.db"
mkdir -p "$CORPUS/nested"

cat > "$CORPUS/hr.md" <<'EOF'
# Employee handbook

## Annual leave

Employees are entitled to twenty business days of paid annual leave each year.
EOF

cat > "$CORPUS/technical.txt" <<'EOF'
The storage engine uses write-ahead logging. The exact reference is DB-ZX-481.
EOF

cat > "$CORPUS/nested/project.md" <<'EOF'
# Project Aurora

The release train leaves on Friday. The launch owner is Morgan.
EOF

printf '%s\n' 'unsupported data must be ignored' > "$CORPUS/ignored.bin"

docdex() {
  "$DOCDEX_BIN" --db "$DB" "$@"
}

heading() {
  printf '\n== %s ==\n' "$1"
}

require_text() {
  local output="$1"
  local expected="$2"
  if [[ "$output" != *"$expected"* ]]; then
    printf 'error: expected output to contain %q\n' "$expected" >&2
    printf '%s\n' "$output" >&2
    exit 1
  fi
}

heading 'Initialize and index'
docdex init
docdex add "$CORPUS"
INDEX_OUTPUT="$(docdex index)"
printf '%s\n' "$INDEX_OUTPUT"
require_text "$INDEX_OUTPUT" '"embedding_jobs": 3'

heading 'Inspect roots and status'
docdex roots
STATUS_OUTPUT="$(docdex status)"
printf '%s\n' "$STATUS_OUTPUT"
require_text "$STATUS_OUTPUT" '"indexed_count": 3'

heading 'Semantic retrieval: vacation wording is absent from the source'
SEMANTIC_OUTPUT="$(docdex search 'How many vacation days do workers get?')"
printf '%s\n' "$SEMANTIC_OUTPUT"
require_text "$SEMANTIC_OUTPUT" 'hr.md'
require_text "$SEMANTIC_OUTPUT" 'twenty business days'

heading 'Exact lexical retrieval'
EXACT_OUTPUT="$(docdex search 'DB-ZX-481')"
printf '%s\n' "$EXACT_OUTPUT"
require_text "$EXACT_OUTPUT" 'technical.txt'

heading 'MCP initialize, tool discovery, and all tool calls'
MCP_OUTPUT="$(printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"retrieve","arguments":{"question":"vacation allowance","limit":3}}}' \
  '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"read_chunks","arguments":{"chunk_ids":[1],"before":0,"after":1}}}' \
  '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"list_documents","arguments":{}}}' \
  '{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"index_status","arguments":{}}}' \
  | docdex mcp)"
printf '%s\n' "$MCP_OUTPUT"
for tool in retrieve read_chunks list_documents index_status; do
  require_text "$MCP_OUTPUT" "$tool"
done
require_text "$MCP_OUTPUT" 'hr.md'

heading 'Modify a document; search performs automatic reconciliation'
sleep 1
cat > "$CORPUS/hr.md" <<'EOF'
# Employee handbook

## Annual leave

Employees are entitled to twenty-five business days of paid annual leave each year.
EOF
MODIFIED_OUTPUT="$(docdex search 'vacation allowance')"
printf '%s\n' "$MODIFIED_OUTPUT"
require_text "$MODIFIED_OUTPUT" 'twenty-five business days'

heading 'Rename without re-embedding'
mv "$CORPUS/technical.txt" "$CORPUS/system.txt"
RENAME_OUTPUT="$(docdex reconcile)"
printf '%s\n' "$RENAME_OUTPUT"
require_text "$RENAME_OUTPUT" '"renamed_documents": 1'
require_text "$RENAME_OUTPUT" '"embedding_jobs": 0'

heading 'Delete and remove stale index state'
rm "$CORPUS/nested/project.md"
DELETE_OUTPUT="$(docdex reconcile)"
printf '%s\n' "$DELETE_OUTPUT"
require_text "$DELETE_OUTPUT" '"deleted_documents": 1'

heading 'Contain a malformed PDF without harming healthy documents'
printf '%s\n' 'not a valid PDF' > "$CORPUS/broken.pdf"
BAD_PDF_OUTPUT="$(docdex reconcile)"
printf '%s\n' "$BAD_PDF_OUTPUT"
require_text "$BAD_PDF_OUTPUT" '"failed_documents": 1'

heading 'Restart-compatible search and database health check'
RESTART_OUTPUT="$(docdex search 'annual time off')"
printf '%s\n' "$RESTART_OUTPUT"
require_text "$RESTART_OUTPUT" 'hr.md'
docdex doctor

heading 'Remove the configured root'
docdex remove "$CORPUS"
ROOTS_OUTPUT="$(docdex roots)"
printf '%s\n' "$ROOTS_OUTPUT"
if [[ "$ROOTS_OUTPUT" == *"$CORPUS"* ]]; then
  printf 'error: root remained configured after removal\n' >&2
  exit 1
fi

printf '\nAll interactive demo checks passed.\n'
printf 'Run `make test-e2e` for the automated text-layer PDF acceptance case.\n'
