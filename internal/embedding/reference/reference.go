// Package reference provides a test-only E5 reference, independent of Chiedi's
// token window construction, native inference wrapper and pooling implementation.
package reference

/*
#cgo CFLAGS: -I${SRCDIR}/../../../.deps/include -I${SRCDIR}/..
#cgo LDFLAGS: ${SRCDIR}/../../../.deps/lib/libtokenizers.a -ldl -lm -lstdc++
#include <stdlib.h>
int reference_hidden(const char *library, const char *model, const char *tokenizer,
                     const char *text, float **values, size_t *count, char **error);
*/
import "C"

import (
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"unsafe"
)

// Vector encodes one short, already-prefixed input with upstream special tokens,
// averages the raw ONNX token vectors in float64, then applies L2 normalization.
func Vector(assets, text string) ([]float32, error) {
	library := "libonnxruntime.so"
	if runtime.GOOS == "darwin" {
		library = "libonnxruntime.dylib"
	}
	paths := []string{filepath.Join(assets, library), filepath.Join(assets, "model.onnx"), filepath.Join(assets, "tokenizer.json"), text}
	args := make([]*C.char, len(paths))
	for i, s := range paths {
		args[i] = C.CString(s)
		defer C.free(unsafe.Pointer(args[i]))
	}
	var values *C.float
	var count C.size_t
	var message *C.char
	if C.reference_hidden(args[0], args[1], args[2], args[3], &values, &count, &message) != 0 {
		if message == nil {
			return nil, fmt.Errorf("reference inference failed")
		}
		defer C.free(unsafe.Pointer(message))
		return nil, fmt.Errorf("reference: %s", C.GoString(message))
	}
	defer C.free(unsafe.Pointer(values))
	if count == 0 || count%384 != 0 {
		return nil, fmt.Errorf("invalid reference output length: %d", count)
	}
	raw := unsafe.Slice(values, int(count))
	mean := make([]float64, 384)
	for i, v := range raw {
		mean[i%384] += float64(v)
	}
	var norm float64
	for i := range mean {
		mean[i] /= float64(len(raw) / 384)
		norm += mean[i] * mean[i]
	}
	if norm == 0 || math.IsNaN(norm) || math.IsInf(norm, 0) {
		return nil, fmt.Errorf("invalid reference norm")
	}
	out := make([]float32, 384)
	for i := range mean {
		out[i] = float32(mean[i] / math.Sqrt(norm))
	}
	return out, nil
}
