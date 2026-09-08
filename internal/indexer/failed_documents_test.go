package indexer

import (
	"context"
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
			absolutePath, err := filepath.Abs(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(stats.FailedDocumentPaths) != 1 {
				t.Fatalf("failed_document_paths=%v, want one path", stats.FailedDocumentPaths)
			}
			if stats.FailedDocumentPaths[0] != absolutePath {
				t.Fatalf("failed_document_paths=%v, want [%q]", stats.FailedDocumentPaths, absolutePath)
			}
		})
	}
}
