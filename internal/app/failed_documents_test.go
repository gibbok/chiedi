package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCLIStatusIncludesPreviouslyFailedPaths(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "index.db")
	root := t.TempDir()
	path := filepath.Join(root, "broken.txt")
	if err := os.WriteFile(path, []byte{0xff}, 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) []byte {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := Run(ctx, append([]string{"--db", db}, args...), bytes.NewReader(nil), &stdout, &stderr); code != 0 {
			t.Fatalf("%v: exit=%d stderr=%s", args, code, stderr.String())
		}
		return stdout.Bytes()
	}
	run("add", root)
	run("index")
	run("reconcile")
	// Each Run opens the database again, also covering persisted status on restart.
	var status struct {
		Counts struct {
			Failed int `json:"failed_count"`
		} `json:"counts"`
		Paths []string `json:"failed_document_paths"`
		Last struct {
			Failed int `json:"failed_documents"`
			Paths []string `json:"failed_document_paths"`
		} `json:"last_reconciliation"`
	}
	if err := json.Unmarshal(run("status"), &status); err != nil {
		t.Fatal(err)
	}
	if status.Counts.Failed != 1 || len(status.Paths) != 1 || status.Paths[0] != want {
		t.Fatalf("status lost the previously failed file: %+v", status)
	}
	if status.Last.Failed != 0 || status.Last.Paths == nil || len(status.Last.Paths) != 0 {
		t.Fatalf("last run should report no new failures: %+v", status.Last)
	}
}
