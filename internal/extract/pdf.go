package extract

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/tetratelabs/wazero"
)

const maxPDFPageBytes = 4 << 20

var pdfRuntime struct {
	sync.Once
	pool pdfium.Pool
	err  error
}

func pdfFile(ctx context.Context, path string) (doc Document, err error) {
	// A malformed document must fail extraction, not terminate an indexing run.
	defer func() {
		if recovered := recover(); recovered != nil {
			doc = Document{}
			err = fmt.Errorf("parse PDF: parser panic: %v", recovered)
		}
	}()
	options, err := pdfOCROptions()
	if err != nil {
		return Document{}, err
	}
	data, err := readFileBounded(path, MaxFileBytes)
	if err != nil {
		return Document{}, err
	}
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	pdfRuntime.Do(func() {
		pdfRuntime.pool, pdfRuntime.err = newPDFPool()
	})
	if pdfRuntime.err != nil {
		return Document{}, fmt.Errorf("initialize PDFium: %w", pdfRuntime.err)
	}
	instance, err := pdfRuntime.pool.GetInstanceWithContext(ctx)
	if err != nil {
		// The pool replaces context cancellation with its own timeout error.
		// Preserve the caller's error identity for errors.Is and cancellation handling.
		if ctx.Err() != nil {
			return Document{}, ctx.Err()
		}
		return Document{}, fmt.Errorf("acquire PDFium: %w", err)
	}
	defer instance.Close()
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	opened, err := instance.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		return Document{}, fmt.Errorf("parse PDF: %w", err)
	}
	defer instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: opened.Document})
	count, err := instance.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: opened.Document})
	if err != nil {
		return Document{}, fmt.Errorf("PDF page count: %w", err)
	}
	if count.PageCount > MaxPDFPages {
		return Document{}, fmt.Errorf("PDF exceeds %d-page limit", MaxPDFPages)
	}
	doc.Title = filepath.Base(path)
	total := 0
	for n := 0; n < count.PageCount; n++ {
		if err := ctx.Err(); err != nil {
			return Document{}, err
		}
		page, err := instance.FPDF_LoadPage(&requests.FPDF_LoadPage{Document: opened.Document, Index: n})
		if err != nil {
			return Document{}, fmt.Errorf("PDF page %d: %w", n+1, err)
		}
		text, extractErr := pdfPage(ctx, instance, requests.Page{ByReference: &page.Page}, options)
		_, closeErr := instance.FPDF_ClosePage(&requests.FPDF_ClosePage{Page: page.Page})
		if extractErr != nil {
			return Document{}, fmt.Errorf("PDF page %d: %w", n+1, extractErr)
		}
		if closeErr != nil {
			return Document{}, fmt.Errorf("close PDF page %d: %w", n+1, closeErr)
		}
		total += len(text)
		if total > MaxExtractedBytes {
			return Document{}, errors.New("PDF extracted text exceeds limit")
		}
		if text != "" {
			doc.Sections = append(doc.Sections, Section{Text: text, PageStart: n + 1, PageEnd: n + 1})
		}
	}
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	if len(doc.Sections) == 0 {
		return Document{}, errors.New("PDF has no readable text (including OCR)")
	}
	return doc, nil
}

func pdfPage(ctx context.Context, instance pdfium.Pdfium, page requests.Page, options ocrOptions) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// Forced OCR must remain usable when the text layer cannot be decoded.
	if options.mode != "always" {
		text, err := pdfPageText(instance, page)
		if err != nil {
			return "", err
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if readablePDFText(text) {
			return text, nil
		}
	}
	objects, err := instance.FPDFPage_CountObjects(&requests.FPDFPage_CountObjects{Page: page})
	if err != nil {
		return "", err
	}
	if objects.Count == 0 {
		// Annotation appearances (for example approval stamps) are visible
		// content but are not included in the normal page-object count.
		annotations, err := instance.FPDFPage_GetAnnotCount(&requests.FPDFPage_GetAnnotCount{Page: page})
		if err != nil {
			return "", err
		}
		if annotations.Count == 0 {
			return "", nil
		}
	}
	return ocrPDFPage(ctx, instance, page, options)
}

func pdfPageText(instance pdfium.Pdfium, page requests.Page) (string, error) {
	loaded, err := instance.FPDFText_LoadPage(&requests.FPDFText_LoadPage{Page: page})
	if err != nil {
		return "", err
	}
	defer instance.FPDFText_ClosePage(&requests.FPDFText_ClosePage{TextPage: loaded.TextPage})
	count, err := instance.FPDFText_CountChars(&requests.FPDFText_CountChars{TextPage: loaded.TextPage})
	if err != nil {
		return "", err
	}
	// Bound allocations before requesting UTF-16 text; each character may need
	// four UTF-8 bytes. PDFium supplies word spacing and reading order.
	if count.Count < 0 || count.Count > maxPDFPageBytes/4 {
		return "", errors.New("PDF page text exceeds limit")
	}
	if count.Count == 0 {
		return "", nil
	}
	result, err := instance.FPDFText_GetText(&requests.FPDFText_GetText{TextPage: loaded.TextPage, Count: count.Count})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(normalize(result.Text)), nil
}

func readablePDFText(text string) bool {
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return true
		}
	}
	return false
}

// Keep PDF parsing offline and prevent native diagnostics from corrupting MCP
// stdout. The WASM module has no filesystem mounts and at most 512 MiB memory.
func newPDFPool() (pdfium.Pool, error) {
	config := wazero.NewRuntimeConfig().WithMemoryLimitPages(8192)
	// Cache only compiled engine code, never document content. A read-only home
	// or unavailable cache must not prevent document conversion.
	if dir, err := os.UserCacheDir(); err == nil {
		if cache, err := wazero.NewCompilationCacheWithDir(filepath.Join(dir, "chiedi", "pdfium")); err == nil {
			config = config.WithCompilationCache(cache)
		}
	}
	return webassembly.Init(webassembly.Config{
		MaxIdle: 1, MaxTotal: 1, FSConfig: wazero.NewFSConfig(),
		RuntimeConfig: config, Stdout: io.Discard, Stderr: io.Discard,
	})
}
