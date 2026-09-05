package extract

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/requests"
)

type ocrOptions struct{ mode, language string }

func pdfOCROptions() (ocrOptions, error) {
	mode := os.Getenv("DOCDEX_PDF_OCR")
	if mode == "" {
		mode = "auto"
	}
	if mode != "auto" && mode != "always" && mode != "off" {
		return ocrOptions{}, errors.New("DOCDEX_PDF_OCR must be auto, always, or off")
	}
	language := os.Getenv("DOCDEX_OCR_LANG")
	if language == "" {
		language = "eng"
	}
	return ocrOptions{mode, language}, nil
}

func ocrPDFPage(ctx context.Context, instance pdfium.Pdfium, page requests.Page, options ocrOptions) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	executable, err := exec.LookPath("tesseract")
	if err != nil {
		return "", errors.New("page requires OCR: install Tesseract and its language data, or supply a searchable PDF")
	}
	// A pixel box bounds raster memory even for pathological PDF page sizes.
	rendered, err := instance.RenderPageInPixels(&requests.RenderPageInPixels{Page: page, Width: 2400, Height: 3200})
	if err != nil {
		return "", fmt.Errorf("render for OCR: %w", err)
	}
	defer rendered.Cleanup()
	var input bytes.Buffer
	if err := png.Encode(&input, rendered.Result.Image); err != nil {
		return "", fmt.Errorf("encode OCR image: %w", err)
	}
	return runTesseract(ctx, executable, &input, options.language)
}

func runTesseract(ctx context.Context, executable string, input *bytes.Buffer, language string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	// Pass image bytes through stdin, with no shell, URLs, or on-disk source copy.
	cmd := exec.CommandContext(ctx, executable, "stdin", "stdout", "-l", language)
	cmd.WaitDelay = time.Second
	cmd.Stdin = input
	output := &limitedOutput{limit: maxPDFPageBytes}
	diagnostics := &limitedOutput{limit: 16 << 10}
	cmd.Stdout, cmd.Stderr = output, diagnostics
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("OCR: %w", ctx.Err())
		}
		return "", fmt.Errorf("Tesseract OCR: %w: %s", err, strings.TrimSpace(diagnostics.String()))
	}
	text := strings.TrimSpace(normalize(output.String()))
	if !readablePDFText(text) {
		return "", errors.New("OCR found no readable text on a nonblank page")
	}
	return text, nil
}

type limitedOutput struct {
	buffer bytes.Buffer
	limit  int
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buffer.Len() {
		return 0, errors.New("OCR output exceeds limit")
	}
	return b.buffer.Write(p)
}

func (b *limitedOutput) String() string { return b.buffer.String() }
