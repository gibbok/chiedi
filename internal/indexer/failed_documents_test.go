package indexer

import (
	"context"
	"encoding/json"
	"reflect"
	"os"
	"path/filepath"
	"testing"

	"github.com/gibbok/local-genius/internal/embedding"
	"github.com/gibbok/local-genius/internal/extract"
	"github.com/gibbok/local-genius/internal/store"
)

func TestFailedDocumentsExposeAbsolutePaths(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		prepare  func(t *testing.T, path string)
	}{
		{
			name:     "malformed PDF",
			filename: "broken.pdf",
			prepare: func(t *testing.T, path string) {
				t.Helper()
				if err := os.WriteFile(path, []byte("not a PDF"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:     "oversized text file",
			filename: "oversized.txt",
			prepare: func(t *testing.T, path string) {
				t.Helper()
				file, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := file.Truncate(extract.MaxFileBytes + 1); err != nil {
					file.Close()
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			path := filepath.Join(root, test.filename)
			test.prepare(t, path)

			database, err := store.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			if err := database.AddRoot(ctx, root); err != nil {
				t.Fatal(err)
			}

			stats, err := (Indexer{
				Store:    database,
				Embedder: embedding.Projection{},
			}).Reconcile(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if stats.Failed != 1 {
				t.Fatalf("failed_documents=%d, want 1", stats.Failed)
			}
			absolutePath, err := filepath.EvalSymlinks(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(stats.FailedDocumentPaths) != 1 {
				t.Fatalf("failed_document_paths=%v, want one path", stats.FailedDocumentPaths)
			}
			if stats.FailedDocumentPaths[0] != absolutePath {
				t.Fatalf("failed_document_paths=%v, want [%q]", stats.FailedDocumentPaths, absolutePath)
			}

			last, err := database.Meta(ctx, "last_reconciliation")
			if err != nil {
				t.Fatal(err)
			}
			var persisted Stats
			if err := json.Unmarshal([]byte(last), &persisted); err != nil {
				t.Fatal(err)
			}
			if persisted.Failed != stats.Failed || !reflect.DeepEqual(persisted.FailedDocumentPaths, stats.FailedDocumentPaths) {
				t.Fatalf("persisted failures disagree: %+v vs %+v", persisted, stats)
			}
			unchanged, err := (Indexer{Store: database, Embedder: embedding.Projection{}}).Reconcile(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if unchanged.Failed != 0 || unchanged.ExtractionJobs != 0 || unchanged.EmbeddingJobs != 0 {
				t.Fatalf("unchanged file did extra work: %+v", unchanged)
			}
			encoded, err := json.Marshal(unchanged)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			if string(fields["failed_document_paths"]) != "[]" {
				t.Fatalf("empty per-run paths must be an array: %s", encoded)
			}
		})
	}
}
