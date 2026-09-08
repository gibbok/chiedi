package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/gibbok/local-genius/internal/embedding"
	"github.com/gibbok/local-genius/internal/indexer"
	"github.com/gibbok/local-genius/internal/store"
)

func TestStatusKeepsCurrentFailedPathsAcrossReconciliation(t *testing.T) {
	ctx := context.Background()
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "status.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	server := Server{Store: database, Indexer: indexer.Indexer{Store: database, Embedder: embedding.Projection{}}}

	check := func(want []string, reconcile bool) {
		t.Helper()
		var result map[string]any
		if reconcile {
			value, rpcErr := server.handle(ctx, request{Method: "tools/call", Params: json.RawMessage(`{"name":"index_status","arguments":{}}`)})
			if rpcErr != nil {
				t.Fatal(rpcErr)
			}
			response := value.(map[string]any)
			if response["isError"] != false {
				t.Fatalf("index_status failed: %+v", response)
			}
			result = response["structuredContent"].(map[string]any)
			// Verify the text payload and structured payload expose identical paths.
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			text := response["content"].([]any)[0].(map[string]any)["text"].(string)
			if string(encoded) != text {
				t.Fatalf("MCP text and structured content disagree: %s vs %s", encoded, text)
			}
		} else {
			var err error
			result, err = Status(ctx, database)
			if err != nil {
				t.Fatal(err)
			}
		}
		got, ok := result["failed_document_paths"].([]string)
		if !ok || got == nil {
			t.Fatalf("missing array of current failed paths: %+v", result)
		}
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) || result["counts"].(store.Counts).Failed != len(want) {
			t.Fatalf("current failure count/paths disagree: %+v, want %v", result, want)
		}
		for _, path := range got {
			if !filepath.IsAbs(path) {
				t.Fatalf("non-absolute failure path: %q", path)
			}
		}
	}

	check([]string{}, false)
	var paths []string
	for n := 0; n < 2; n++ {
		root := t.TempDir()
		dir := filepath.Join(root, "nested folder")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "chybný dokument.txt")
		if err := os.WriteFile(path, []byte{0xff}, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := database.AddRoot(ctx, root); err != nil {
			t.Fatal(err)
		}
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	check(append([]string{}, paths...), true)
	check(append([]string{}, paths...), true)
	last, err := database.Meta(ctx, "last_reconciliation")
	if err != nil {
		t.Fatal(err)
	}
	var stats indexer.Stats
	if err := json.Unmarshal([]byte(last), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.Failed != 0 || len(stats.FailedDocumentPaths) != 0 || stats.ExtractionJobs != 0 || stats.EmbeddingJobs != 0 {
		t.Fatalf("unchanged run must not recount or reprocess old failures: %+v", stats)
	}
	// Old databases may have last-run metadata without the new path field.
	if err := database.SetMeta(ctx, "last_reconciliation", `{"failed_documents":2}`); err != nil {
		t.Fatal(err)
	}
	check(append([]string{}, paths...), false)

	renamed := filepath.Join(filepath.Dir(paths[0]), "renamed.txt")
	if err := os.Rename(paths[0], renamed); err != nil {
		t.Fatal(err)
	}
	paths[0] = renamed
	check(append([]string{}, paths...), true)
	if err := os.WriteFile(paths[0], []byte("repaired UTF-8 document"), 0o600); err != nil {
		t.Fatal(err)
	}
	check([]string{paths[1]}, true)
	if err := os.Remove(paths[1]); err != nil {
		t.Fatal(err)
	}
	check([]string{}, true)
	check([]string{}, false)
}
