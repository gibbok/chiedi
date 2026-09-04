package retrieval

import "testing"

func TestFTSQuery(t *testing.T){got:=ftsQuery(`Reference ZX-481`);if got!=`"reference" OR "zx" OR "481"`{t.Fatalf("got %q",got)}}
