package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
)

type beforeFailedPathsReader struct {
	sqlReader
	before func()
}

func (r beforeFailedPathsReader) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	r.before()
	return r.sqlReader.QueryContext(ctx, query, args...)
}

func TestCountsWithFailedPathsKeepsSnapshotDuringConcurrentWrite(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "snapshot.db")
	reader, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	writer, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err := reader.AddRoot(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	roots, err := reader.Roots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	replacement := Replacement{RootID: roots[0].ID, RelativePath: "failed.txt", Format: "txt", Status: "failed", Error: "invalid text"}
	if _, err := writer.ReplaceDocument(ctx, replacement); err != nil {
		t.Fatal(err)
	}
	tx, err := reader.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	wrote := false
	hooked := beforeFailedPathsReader{sqlReader: tx, before: func() {
		// Commit a repair after all counts are read but before paths are read.
		replacement.Status = "indexed"
		replacement.Error = ""
		if _, err := writer.ReplaceDocument(ctx, replacement); err != nil {
			t.Fatal(err)
		}
		wrote = true
	}}
	counts, paths, err := countsWithFailedPaths(ctx, hooked)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(roots[0].Path, "failed.txt")}
	if !wrote || counts.Documents != 1 || counts.Failed != 1 || counts.Indexed != 0 || !reflect.DeepEqual(paths, want) {
		t.Fatalf("snapshot mixed counts and paths: %+v %v; write=%v", counts, paths, wrote)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	// A new status snapshot must see the committed repair.
	counts, paths, err = reader.CountsWithFailedPaths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Documents != 1 || counts.Failed != 0 || counts.Indexed != 1 || paths == nil || len(paths) != 0 {
		t.Fatalf("new snapshot missed repair: %+v %v", counts, paths)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := reader.CountsWithFailedPaths(cancelled); err == nil {
		t.Fatal("cancelled snapshot unexpectedly succeeded")
	}
}
