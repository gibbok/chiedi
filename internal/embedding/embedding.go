// Package embedding provides offline multilingual document and query embeddings.
package embedding

import (
	"context"
	"errors"
	"math"
)

const dimensions = 384

// Embed embeds passages. QueryEmbedder distinguishes the asymmetric query task.
type Embedder interface {
	ID() string
	Dimensions() int
	Embed(context.Context, []string) ([][]float32, error)
}

type QueryEmbedder interface {
	EmbedQuery(context.Context, []string) ([][]float32, error)
}

// EmbedQueries allows deterministic test doubles without a separate query method.
func EmbedQueries(ctx context.Context, e Embedder, texts []string) ([][]float32, error) {
	if q, ok := e.(QueryEmbedder); ok {
		return q.EmbedQuery(ctx, texts)
	}
	return e.Embed(ctx, texts)
}

// E5 uses one lazy, serialized native session per process. The OS releases that
// process-owned session at exit; individual calls release all temporary tensors.
// Merely constructing E5 (status, unchanged reconciliation) loads no model.
type E5 struct{}

func (E5) ID() string      { return "intfloat/multilingual-e5-small-ccc66d3-int8-window-v1" }
func (E5) Dimensions() int { return dimensions }
func (E5) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return embedTexts(ctx, texts, "passage: ")
}
func (E5) EmbedQuery(ctx context.Context, texts []string) ([][]float32, error) {
	return embedTexts(ctx, texts, "query: ")
}

// Cosine returns cosine similarity for normalized or unnormalized vectors.
func Cosine(a, b []float32) (float64, error) {
	if len(a) == 0 || len(a) != len(b) {
		return 0, errors.New("vectors must have the same non-zero dimensions")
	}
	var dot, aa, bb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		if math.IsNaN(x) || math.IsInf(x, 0) || math.IsNaN(y) || math.IsInf(y, 0) {
			return 0, errors.New("vectors must contain only finite values")
		}
		dot += x * y
		aa += x * x
		bb += y * y
	}
	if aa == 0 || bb == 0 {
		return 0, nil
	}
	return dot / math.Sqrt(aa*bb), nil
}

func normalize(v []float32) error {
	var norm float64
	for _, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return errors.New("non-finite model output")
		}
		norm += float64(x) * float64(x)
	}
	if norm == 0 {
		return errors.New("zero model output")
	}
	scale := 1 / math.Sqrt(norm)
	for i := range v {
		v[i] = float32(float64(v[i]) * scale)
	}
	return nil
}
