package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/tetratelabs/wazero"
)

// Valid PDF documents (including JPEG image streams) are generated only in
// t.TempDir. No corpus bytes or network downloads are part of the repository.
func TestBuiltBinaryPDFConversion(t *testing.T) {
	if os.Getenv("DOCDEX_TEST_OCR") != "1" {
		t.Skip("DOCDEX_TEST_OCR=1 requires real PDF/OCR E2E coverage")
	}
	if _, err := exec.LookPath("tesseract"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCDEX_PDF_OCR", "auto")
	t.Setenv("DOCDEX_OCR_LANG", "eng")
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	dir := t.TempDir()
	binary, db := filepath.Join(dir, "docdex"), filepath.Join(dir, "index.db")
	// Include headroom for a cold production build; the test still bounds all
	// CLI/MCP subprocesses so a hung conversion cannot stall the test suite.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/docdex")
	build.Dir = repo
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	corpus := filepath.Join(dir, "corpus")
	if err := os.Mkdir(corpus, 0700); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, binary, append([]string{"--db", db}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	pool, err := webassembly.Init(webassembly.Config{MaxIdle: 1, MaxTotal: 1, FSConfig: wazero.NewFSConfig(), Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	writePDF(t, filepath.Join(corpus, "digital.pdf"), "The digital contract reference is DIGITAL772.")
	writeMixedPDF(t, pool, filepath.Join(corpus, "mixed.pdf"), "forty")
	write(t, filepath.Join(corpus, "broken.pdf"), "invalid PDF document")
	wantJobs := 3
	// Optional independently authored W3C PDF, downloaded into temp/ by the
	// caller. Tests stay offline and normal CI never depends on that website.
	if publicPath := os.Getenv("DOCDEX_TEST_PUBLIC_PDF"); publicPath != "" {
		data, err := os.ReadFile(publicPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(corpus, "public.pdf"), data, 0600); err != nil {
			t.Fatal(err)
		}
		wantJobs++
	}
	run("init")
	run("add", corpus)
	var stats struct {
		Failed    int `json:"failed_documents"`
		Extracted int `json:"extraction_jobs"`
		Embedded  int `json:"embedding_jobs"`
		Reused    int `json:"reused_chunks"`
	}
	if err := json.Unmarshal([]byte(run("index")), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.Failed != 1 || stats.Extracted != wantJobs || stats.Embedded != wantJobs {
		t.Fatalf("index stats: %+v", stats)
	}
	for _, tc := range []struct{ query, path, text string }{{"DIGITAL772", "digital.pdf", "DIGITAL772"}, {"SCAN882", "mixed.pdf", "forty days"}} {
		out := run("search", tc.query)
		first := strings.SplitN(out, "\n\n", 2)[0]
		if !strings.Contains(first, tc.path) || !strings.Contains(first, tc.text) {
			t.Fatalf("PDF retrieval %s: %s", tc.query, out)
		}
	}
	if wantJobs == 4 {
		if out := run("search", "Dummy PDF file"); !strings.Contains(strings.SplitN(out, "\n\n", 2)[0], "public.pdf") {
			t.Fatalf("public PDF retrieval: %s", out)
		}
	}
	assertPDFMCP(t, ctx, binary, db, "forty", nil)
	if err := json.Unmarshal([]byte(run("reconcile")), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.Extracted != 0 || stats.Embedded != 0 {
		t.Fatalf("unchanged PDFs reprocessed: %+v", stats)
	}
	// Replace actual image bytes, then verify updated OCR text is searchable
	// and the unchanged digital cover's embedding is reused.
	writeMixedPDF(t, pool, filepath.Join(corpus, "mixed.pdf"), "sixty")
	mtime := time.Now().Add(time.Second)
	if err := os.Chtimes(filepath.Join(corpus, "mixed.pdf"), mtime, mtime); err != nil {
		t.Fatal(err)
	}
	// Let MCP itself trigger OCR, so parser/OCR diagnostics cannot silently
	// corrupt its JSON protocol. Inspect work before read_chunks reconciles again.
	assertPDFMCP(t, ctx, binary, db, "sixty", func() {
		var status struct {
			Last json.RawMessage `json:"last_reconciliation"`
		}
		if err := json.Unmarshal([]byte(run("status")), &status); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(status.Last, &stats); err != nil {
			t.Fatal(err)
		}
		if stats.Extracted != 1 || stats.Embedded != 1 || stats.Reused != 1 {
			t.Fatalf("MCP changed scanned page stats: %+v", stats)
		}
	})
	if out := run("doctor"); !strings.HasPrefix(out, "ok:") {
		t.Fatalf("database integrity: %s", out)
	}
}

func assertPDFMCP(t *testing.T, ctx context.Context, binary, db, notice string, afterRetrieve func()) {
	t.Helper()
	call := func(name string, args any) json.RawMessage {
		t.Helper()
		request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
		cmd := exec.CommandContext(ctx, binary, "--db", db, "mcp")
		cmd.Stdin = strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{}}\n" + string(request) + "\n")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("MCP: %v %s", err, stderr.String())
		}
		lines := bytes.Split(bytes.TrimSpace(stdout.Bytes()), []byte("\n"))
		if len(lines) != 2 {
			t.Fatalf("MCP stdout corrupted: %s", stdout.String())
		}
		var response struct {
			Error  any
			Result struct {
				IsError           bool
				StructuredContent json.RawMessage
			}
		}
		if err := json.Unmarshal(lines[1], &response); err != nil {
			t.Fatal(err)
		}
		if response.Error != nil || response.Result.IsError || len(response.Result.StructuredContent) == 0 {
			t.Fatalf("MCP %s failed: %s", name, lines[1])
		}
		return response.Result.StructuredContent
	}
	type result struct {
		ID        int64 `json:"chunk_id"`
		Path      string
		PageStart int `json:"page_start"`
		PageEnd   int `json:"page_end"`
		Text      string
	}
	var retrieved struct{ Results []result }
	if err := json.Unmarshal(call("retrieve", map[string]any{"question": "SCAN882", "limit": 3}), &retrieved); err != nil {
		t.Fatal(err)
	}
	if len(retrieved.Results) == 0 {
		t.Fatal("no scanned PDF results")
	}
	first := retrieved.Results[0]
	if first.Path != "mixed.pdf" || first.PageStart != 3 || first.PageEnd != 3 || !strings.Contains(first.Text, notice+" days") {
		t.Fatalf("scan citation/content: %+v", first)
	}
	if afterRetrieve != nil {
		afterRetrieve()
	}
	read := call("read_chunks", map[string]any{"chunk_ids": []int64{first.ID}, "before": 0, "after": 0})
	if !bytes.Contains(read, []byte(notice+" days")) || !bytes.Contains(read, []byte(`"page_start":3`)) {
		t.Fatalf("read_chunks lost PDF content/provenance: %s", read)
	}
}

func writeMixedPDF(t *testing.T, pool pdfium.Pool, path, notice string) {
	t.Helper()
	data := pdfFixture([]string{"BT /F1 24 Tf 72 720 Td (SCANNED CONTRACT SCAN882) Tj 0 -40 Td (Notice period is " + notice + " days.) Tj ET"}, nil)
	instance, err := pool.GetInstanceWithContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	doc, err := instance.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := instance.RenderPageInPixels(&requests.RenderPageInPixels{Page: requests.Page{ByIndex: &requests.PageByIndex{Document: doc.Document, Index: 0}}, Width: 1224, Height: 1584})
	if err != nil {
		t.Fatal(err)
	}
	defer rendered.Cleanup()
	var scan bytes.Buffer
	if err := jpeg.Encode(&scan, rendered.Result.Image, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	mixed := pdfFixture([]string{"BT /F1 12 Tf 72 720 Td (Digital cover MIXED773.) Tj ET", "1 1 1 rg 0 0 612 792 re f", "q 612 0 0 792 0 0 cm /Im1 Do Q"}, scan.Bytes())
	if err := os.WriteFile(path, mixed, 0600); err != nil {
		t.Fatal(err)
	}
}

func pdfFixture(streams []string, scan []byte) []byte {
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"}
	var kids []string
	for _, stream := range streams {
		id := len(objects) + 1
		kids = append(kids, fmt.Sprintf("%d 0 R", id))
		resources := "/Font << /F1 3 0 R >>"
		if scan != nil {
			resources += fmt.Sprintf(" /XObject << /Im1 %d 0 R >>", 4+2*len(streams))
		}
		objects = append(objects, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << %s >> /Contents %d 0 R >>", resources, id+1), fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	}
	objects[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(streams))
	if scan != nil {
		objects = append(objects, fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 1224 /Height 1584 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream", len(scan), scan))
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	var offsets []int
	for n, obj := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", n+1, obj)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}
