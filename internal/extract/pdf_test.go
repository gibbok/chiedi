package extract

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/responses"
)

func TestPDFPaintedBlankPage(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	blankScan := image.NewRGBA(image.Rect(0, 0, 1224, 1584))
	draw.Draw(blankScan, blankScan.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, blankScan, nil); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"auto", "off"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("DOCDEX_PDF_OCR", mode)
			path := writeTestPDF(t, []string{
				"BT /F1 12 Tf 72 720 Td (First page.) Tj ET",
				"1 1 1 rg 0 0 612 792 re f",
				"q 612 0 0 792 0 0 cm /Im1 Do Q",
				"BT /F1 12 Tf 72 720 Td (Third page.) Tj ET",
			}, encoded.Bytes())
			doc, err := File(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			if len(doc.Sections) != 2 || doc.Sections[1].PageStart != 4 {
				t.Fatalf("blank page lost provenance: %+v", doc)
			}
		})
	}
}

func TestPDFBlankRasterKeepsFaintContent(t *testing.T) {
	for _, tc := range []struct {
		name  string
		color color.RGBA
		blank bool
	}{
		{"white", color.RGBA{255, 255, 255, 255}, true},
		{"transparent", color.RGBA{}, true},
		{"faint gray", color.RGBA{254, 254, 254, 255}, false},
		{"faint red", color.RGBA{255, 254, 254, 255}, false},
		{"faint translucent", color.RGBA{0, 0, 0, 1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := image.NewRGBA(image.Rect(0, 0, 8, 8))
			draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
			img.SetRGBA(4, 4, tc.color)
			// Subimages exercise nonzero bounds and a stride wider than the raster.
			if got := blankPDFRaster(img.SubImage(image.Rect(2, 2, 6, 6)).(*image.RGBA)); got != tc.blank {
				t.Fatalf("blank=%v want=%v", got, tc.blank)
			}
		})
	}
}

type unreadableTextPDF struct{ pdfium.Pdfium }

func (p unreadableTextPDF) FPDFText_LoadPage(*requests.FPDFText_LoadPage) (*responses.FPDFText_LoadPage, error) {
	return nil, errors.New("broken text layer")
}

func TestPDFForcedOCRBypassesBrokenTextLayer(t *testing.T) {
	// A genuinely blank rendering also makes this regression independent of
	// an installed Tesseract. The wrapper simulates a text decoder failure.
	data := testPDF([]string{"1 1 1 rg 0 0 612 792 re f"}, nil)
	pdfRuntime.Do(func() { pdfRuntime.pool, pdfRuntime.err = newPDFPool() })
	if pdfRuntime.err != nil {
		t.Fatal(pdfRuntime.err)
	}
	instance, err := pdfRuntime.pool.GetInstanceWithContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	doc, err := instance.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		t.Fatal(err)
	}
	page := requests.Page{ByIndex: &requests.PageByIndex{Document: doc.Document, Index: 0}}
	text, err := pdfPage(context.Background(), unreadableTextPDF{instance}, page, ocrOptions{mode: "always", language: "eng"})
	if err != nil || text != "" {
		t.Fatalf("forced OCR still depends on text decoding: %q, %v", text, err)
	}
}

func TestMain(m *testing.M) {
	// A real subprocess exercises exec cancellation and bounded pipe copies
	// without requiring a shell or an installed OCR engine for these cases.
	switch os.Getenv("DOCDEX_TEST_OCR_HELPER") {
	case "empty":
		os.Exit(0)
	case "fail":
		fmt.Fprint(os.Stderr, "missing language data")
		os.Exit(1)
	case "large":
		fmt.Fprint(os.Stdout, strings.Repeat("x", maxPDFPageBytes+1))
		os.Exit(0)
	case "wait":
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestOCRProcessErrorsAndCancellation(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ mode, want string }{
		{"empty", "no readable text"}, {"fail", "missing language data"}, {"large", "exceeds limit"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Setenv("DOCDEX_TEST_OCR_HELPER", tc.mode)
			if _, err := runTesseract(context.Background(), executable, &bytes.Buffer{}, "eng"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v", err)
			}
		})
	}
	t.Setenv("DOCDEX_TEST_OCR_HELPER", "wait")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := runTesseract(ctx, executable, &bytes.Buffer{}, "eng"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("OCR subprocess was not terminated promptly")
	}
}

func TestPDFCompressedUnicodeMapping(t *testing.T) {
	t.Setenv("DOCDEX_PDF_OCR", "off")
	var stream bytes.Buffer
	z := zlib.NewWriter(&stream)
	fmt.Fprint(z, "BT /F1 12 Tf 72 720 Td <4180204281> Tj ET")
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	cmap := "/CIDInit /ProcSet findresource begin 12 dict begin begincmap /CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def /CMapName /Test def /CMapType 2 def 1 begincodespacerange <00> <FF> endcodespacerange 4 beginbfchar <41> <0041> <42> <0042> <80> <017E> <81> <03A9> endbfchar endcmap CMapName currentdict /CMap defineresource pop end end"
	data := testPDFObjects([]string{
		"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d /Filter /FlateDecode >>\nstream\n%s\nendstream", stream.Len(), stream.Bytes()),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /ToUnicode 6 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmap), cmap),
	})
	path := filepath.Join(t.TempDir(), "unicode.pdf")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	doc, err := File(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.Sections[0].Text, "Až BΩ") {
		t.Fatalf("Unicode mapping lost: %+v", doc.Sections)
	}
}

func TestPDFWordSpacingAndPageProvenance(t *testing.T) {
	t.Setenv("DOCDEX_PDF_OCR", "off")
	path := writeTestPDF(t, []string{
		"BT /F1 12 Tf 72 720 Td [(Con) 0 (tract) -278 (reference)] TJ 0 -20 Td (First page.) Tj ET",
		"",
		"BT /F1 12 Tf 72 720 Td (Third page reference ZX-903.) Tj ET",
	}, nil)
	doc, err := File(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Sections) != 2 {
		t.Fatalf("sections: %+v", doc.Sections)
	}
	if !strings.Contains(doc.Sections[0].Text, "Contract reference\nFirst page.") {
		t.Fatalf("broken words/lines: %q", doc.Sections[0].Text)
	}
	if doc.Sections[0].PageStart != 1 || doc.Sections[0].PageEnd != 1 || doc.Sections[1].PageStart != 3 || doc.Sections[1].PageEnd != 3 {
		t.Fatalf("page provenance: %+v", doc.Sections)
	}
}

func TestPDFInvalidBlankAndPageLimit(t *testing.T) {
	t.Setenv("DOCDEX_PDF_OCR", "off")
	for _, tc := range []struct {
		name string
		data []byte
		want string
	}{
		{"malformed", []byte("not a PDF"), "parse PDF"},
		{"blank", testPDF([]string{""}, nil), "no readable text"},
		{"too-many-pages", testPDF(make([]string, MaxPDFPages+1), nil), "page limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "input.pdf")
			if err := os.WriteFile(path, tc.data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := File(context.Background(), path); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q; got %v", tc.want, err)
			}
		})
	}
}

func TestPDFCancellationAndPoolWait(t *testing.T) {
	path := writeTestPDF(t, []string{"BT /F1 12 Tf 72 720 Td (Ready.) Tj ET"}, nil)
	if _, err := File(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	instance, err := pdfRuntime.pool.GetInstanceWithContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := File(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("pool acquisition must preserve the caller's deadline error: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("pool did not honor cancellation")
	}
	waiting, cancelWaiting := context.WithCancel(context.Background())
	defer cancelWaiting()
	timer := time.AfterFunc(20*time.Millisecond, cancelWaiting)
	defer timer.Stop()
	if _, err := File(waiting, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("pool acquisition must preserve explicit cancellation: %v", err)
	}
	cancelled, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	if _, err := File(cancelled, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestPDFMixedScanDoesNotSilentlyLosePage(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("DOCDEX_PDF_OCR", "auto")
	scan := testScan(t)
	path := writeTestPDF(t, []string{"BT /F1 12 Tf 72 720 Td (Text page.) Tj ET", "q 540 0 0 700 36 46 cm /Im1 Do Q"}, scan)
	doc, err := File(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "PDF page 2") || !strings.Contains(err.Error(), "install Tesseract") {
		t.Fatalf("got %+v, %v", doc, err)
	}
	if len(doc.Sections) != 0 {
		t.Fatal("returned a silently incomplete document")
	}
	t.Setenv("DOCDEX_PDF_OCR", "off")
	if _, err := File(context.Background(), path); err == nil || !strings.Contains(err.Error(), "enable local OCR") {
		t.Fatalf("got %v", err)
	}
}

func TestPDFLocalOCR(t *testing.T) {
	if os.Getenv("DOCDEX_TEST_OCR") != "1" {
		t.Skip("set DOCDEX_TEST_OCR=1 to require real Tesseract acceptance")
	}
	if _, err := exec.LookPath("tesseract"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCDEX_PDF_OCR", "auto")
	t.Setenv("DOCDEX_OCR_LANG", "eng")
	path := writeTestPDF(t, []string{"BT /F1 12 Tf 72 720 Td (First digital page.) Tj ET", "", "q 540 0 0 700 36 46 cm /Im1 Do Q"}, testScan(t))
	doc, err := File(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Sections) != 2 || doc.Sections[1].PageStart != 3 || !strings.Contains(doc.Sections[1].Text, "SCANNED CONTRACT") || !strings.Contains(doc.Sections[1].Text, "thirty days") {
		t.Fatalf("OCR or provenance: %+v", doc.Sections)
	}
	t.Setenv("DOCDEX_PDF_OCR", "always")
	path = writeTestPDF(t, []string{"BT /F1 24 Tf 72 720 Td (FORCED OCR CONTRACT) Tj ET"}, nil)
	doc, err = File(context.Background(), path)
	if err != nil || !strings.Contains(doc.Sections[0].Text, "FORCED OCR CONTRACT") {
		t.Fatalf("forced OCR: %+v, %v", doc, err)
	}
}

func TestPDFOCROptionsAndOutputLimit(t *testing.T) {
	t.Setenv("DOCDEX_PDF_OCR", "invalid")
	if _, err := pdfOCROptions(); err == nil {
		t.Fatal("invalid mode accepted")
	}
	out := &limitedOutput{limit: 4}
	if _, err := out.Write([]byte("1234")); err != nil {
		t.Fatal(err)
	}
	if _, err := out.Write([]byte("5")); err == nil || out.String() != "1234" {
		t.Fatal("output limit not enforced")
	}
	if _, err := io.Copy(out, strings.NewReader("more")); err == nil {
		t.Fatal("io.Copy bypassed the output limit")
	}
}

// Generate every document inside t.TempDir; no PDFs or scan data live in Git.
func writeTestPDF(t *testing.T, streams []string, scan []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.pdf")
	if err := os.WriteFile(path, testPDF(streams, scan), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testPDF(streams []string, scan []byte) []byte {
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"}
	var kids []string
	imageID := 4 + 2*len(streams)
	for _, stream := range streams {
		id := len(objects) + 1
		kids = append(kids, fmt.Sprintf("%d 0 R", id))
		resources := "/Font << /F1 3 0 R >>"
		if scan != nil {
			resources += fmt.Sprintf(" /XObject << /Im1 %d 0 R >>", imageID)
		}
		objects = append(objects, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << %s >> /Contents %d 0 R >>", resources, id+1), fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	}
	objects[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(streams))
	if scan != nil {
		objects = append(objects, fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 1224 /Height 1584 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream", len(scan), scan))
	}
	return testPDFObjects(objects)
}

func testPDFObjects(objects []string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for n, obj := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", n+1, obj)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

func testScan(t *testing.T) []byte {
	t.Helper()
	data := testPDF([]string{"BT /F1 24 Tf 72 720 Td (SCANNED CONTRACT) Tj 0 -40 Td (Notice period is thirty days.) Tj ET"}, nil)
	pdfRuntime.Do(func() { pdfRuntime.pool, pdfRuntime.err = newPDFPool() })
	if pdfRuntime.err != nil {
		t.Fatal(pdfRuntime.err)
	}
	instance, err := pdfRuntime.pool.GetInstanceWithContext(context.Background())
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
	var b bytes.Buffer
	if err := jpeg.Encode(&b, rendered.Result.Image, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
