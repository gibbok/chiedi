package embedding

/*
#cgo CFLAGS: -I${SRCDIR}/../../.deps/include
#cgo LDFLAGS: ${SRCDIR}/../../.deps/lib/libtokenizers.a -ldl -lm -lstdc++
#include <stdlib.h>
#include "native.h"
*/
import "C"

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"
)

var nativeGate = make(chan struct{}, 1)
var processEngine *C.ChiediEngine

// assetDirectory resolves symlinks so an installed ~/.local/bin/chiedi can find
// its assets under ~/.local/lib/chiedi. It never searches the working directory.
func assetDirectory() (string, error) {
	if dir := os.Getenv("CHIEDI_ASSETS"); dir != "" {
		return filepath.Abs(dir)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), "assets"), nil
}

type assetManifest struct {
	ModelID string            `json:"model_id"`
	Files   map[string]string `json:"files"`
}

func verifyAssets(dir string) error {
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return err
	}
	var m assetManifest
	if err = json.Unmarshal(raw, &m); err != nil {
		return err
	}
	if m.ModelID != (E5{}).ID() {
		return fmt.Errorf("asset model identity mismatch: %q", m.ModelID)
	}
	for _, name := range []string{"model.onnx", "tokenizer.json", runtimeLibrary()} {
		expected := m.Files[name]
		if len(expected) != 64 {
			return fmt.Errorf("missing checksum for %s", name)
		}
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if hex.EncodeToString(h.Sum(nil)) != expected {
			return fmt.Errorf("checksum mismatch for %s", name)
		}
	}
	return nil
}

func runtimeLibrary() string {
	if runtime.GOOS == "darwin" {
		return "libonnxruntime.dylib"
	}
	return "libonnxruntime.so"
}

func nativeError(message *C.char) error {
	if message == nil {
		return fmt.Errorf("native embedding operation failed")
	}
	defer C.free(unsafe.Pointer(message))
	return fmt.Errorf("%s", C.GoString(message))
}

func openNative(dir string) (*C.ChiediEngine, error) {
	if err := verifyAssets(dir); err != nil {
		return nil, fmt.Errorf("embedding assets in %s: %w; run make setup or reinstall the complete bundle", dir, err)
	}
	library := C.CString(filepath.Join(dir, runtimeLibrary()))
	model := C.CString(filepath.Join(dir, "model.onnx"))
	tokenizer := C.CString(filepath.Join(dir, "tokenizer.json"))
	defer C.free(unsafe.Pointer(library))
	defer C.free(unsafe.Pointer(model))
	defer C.free(unsafe.Pointer(tokenizer))
	var message *C.char
	e := C.chiedi_open(library, model, tokenizer, &message)
	if e == nil {
		return nil, nativeError(message)
	}
	return e, nil
}

func nativeTokens(e *C.ChiediEngine, text string) ([]int64, error) {
	// C strings cannot carry NUL; preserve it as a separator instead of silently
	// losing the rest of a document. The same normalization applies to queries.
	input := C.CString(strings.ReplaceAll(text, "\x00", " "))
	defer C.free(unsafe.Pointer(input))
	var ids *C.int64_t
	var count C.size_t
	var message *C.char
	if C.chiedi_tokens(e, input, &ids, &count, &message) != 0 {
		return nil, nativeError(message)
	}
	defer C.free(unsafe.Pointer(ids))
	if count == 0 {
		return nil, fmt.Errorf("tokenizer returned no tokens")
	}
	raw := unsafe.Slice(ids, int(count))
	out := make([]int64, len(raw))
	for i, v := range raw {
		out[i] = int64(v)
	}
	return out, nil
}

// tokenWindows retains every token and repeats the model's task prefix for each
// window. CLS=0 and SEP=2 are fixed by this pinned XLM-R tokenizer.
func tokenWindows(ids []int64, prefixLength int) [][]int64 {
	prefix := ids[:prefixLength]
	body := ids[prefixLength:]
	capacity := 512 - prefixLength - 2
	var windows [][]int64
	for start := 0; start < len(body) || start == 0; start += capacity {
		end := min(start+capacity, len(body))
		window := make([]int64, 0, 2+prefixLength+end-start)
		window = append(window, 0)
		window = append(window, prefix...)
		window = append(window, body[start:end]...)
		window = append(window, 2)
		windows = append(windows, window)
		if end == len(body) {
			break
		}
	}
	return windows
}

func embedTexts(ctx context.Context, texts []string, prefix string) ([][]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if texts == nil {
		return nil, nil
	}
	out := make([][]float32, len(texts))
	if len(texts) == 0 {
		return out, nil
	}
	select {
	case nativeGate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-nativeGate }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if processEngine == nil {
		dir, err := assetDirectory()
		if err != nil {
			return nil, err
		}
		e, err := openNative(dir)
		if err != nil {
			return nil, err
		}
		processEngine = e
	}
	prefixIDs, err := nativeTokens(processEngine, strings.TrimSpace(prefix))
	if err != nil {
		return nil, err
	}
	for i, text := range texts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out[i] = make([]float32, dimensions)
		if strings.TrimSpace(text) == "" {
			continue
		}
		ids, err := nativeTokens(processEngine, prefix+text)
		if err != nil {
			return nil, err
		}
		if len(ids) < len(prefixIDs) {
			return nil, fmt.Errorf("invalid tokenized prefix")
		}
		for j, id := range prefixIDs {
			if ids[j] != id {
				return nil, fmt.Errorf("tokenizer task prefix mismatch")
			}
		}
		for _, window := range tokenWindows(ids, len(prefixIDs)) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			v := make([]float32, dimensions)
			var message *C.char
			// All Go memory passed to C is pointer-free and retained only for this call.
			status := C.chiedi_run(processEngine, (*C.int64_t)(unsafe.Pointer(&window[0])), C.size_t(len(window)), (*C.float)(unsafe.Pointer(&v[0])), &message)
			if status != 0 {
				return nil, nativeError(message)
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if err := normalize(v); err != nil {
				return nil, err
			}
			weight := float32(max(1, len(window)-len(prefixIDs)-2))
			for d := range v {
				out[i][d] += weight * v[d]
			}
		}
		if err := normalize(out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}
