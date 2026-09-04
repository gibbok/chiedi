package embedding

import (
	"context"
	"errors"
	"hash/fnv"
	"math"
	"strings"
	"unicode"
)

const dimensions = 384

// Embedder is the replaceable local embedding boundary.
type Embedder interface {
	ID() string
	Dimensions() int
	Embed(context.Context, []string) ([][]float32, error)
}

// Projection is a compact, deterministic local semantic projection model. It
// hashes normalized lexical and concept features into a dense signed vector.
// The concept normalization gives useful English synonym recall without a
// model download, while the interface permits a GGUF-backed model later.
type Projection struct{}

func (Projection) ID() string      { return "builtin-semantic-projection-en-v1" }
func (Projection) Dimensions() int { return dimensions }

func (Projection) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if texts == nil {
		return nil, nil
	}
	out := make([][]float32, len(texts))
	for i, text := range texts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out[i] = project(text)
	}
	return out, nil
}

func project(text string) []float32 {
	v := make([]float32, dimensions)
	tokens := tokenize(text)
	for i, token := range tokens {
		add(v, "word:"+token, 1)
		concept := canonical(token)
		add(v, "concept:"+concept, 1.7)
		if i > 0 {
			add(v, "pair:"+canonical(tokens[i-1])+"_"+concept, .35)
		}
	}
	var norm float64
	for _, x := range v {
		norm += float64(x * x)
	}
	if norm > 0 {
		scale := float32(1 / math.Sqrt(norm))
		for i := range v {
			v[i] *= scale
		}
	}
	return v
}

func add(v []float32, feature string, weight float32) {
	h := fnv.New64a()
	_, _ = h.Write([]byte(feature))
	n := h.Sum64()
	idx := int(n % uint64(len(v)))
	if n&(1<<63) != 0 {
		weight = -weight
	}
	v[idx] += weight
}

func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}

var concepts = map[string]string{
	"vacation": "leave", "vacations": "leave", "holiday": "leave", "holidays": "leave",
	"leave": "leave", "annual": "leave", "pto": "leave",
	"worker": "employee", "workers": "employee", "employee": "employee", "employees": "employee", "staff": "employee",
	"receive": "entitlement", "receives": "entitlement", "get": "entitlement", "gets": "entitlement",
	"entitled": "entitlement", "entitlement": "entitlement", "eligible": "entitlement",
	"terminate": "termination", "terminated": "termination", "termination": "termination", "cancel": "termination", "cancellation": "termination",
	"agreement": "contract", "agreements": "contract", "contract": "contract", "contracts": "contract",
	"purchase": "buy", "purchased": "buy", "buy": "buy", "bought": "buy",
	"cost": "price", "costs": "price", "price": "price", "pricing": "price",
	"deadline": "due", "due": "due", "expires": "due", "expiry": "due",
	"rapid": "fast", "quick": "fast", "quickly": "fast", "fast": "fast", "performance": "fast",
}

func canonical(token string) string {
	if c, ok := concepts[token]; ok {
		return c
	}
	for _, suffix := range []string{"ingly", "edly", "ing", "ed", "es", "s"} {
		if strings.HasSuffix(token, suffix) && len(token) > len(suffix)+3 {
			return strings.TrimSuffix(token, suffix)
		}
	}
	return token
}

// Cosine returns cosine similarity for normalized or unnormalized vectors.
func Cosine(a, b []float32) (float64, error) {
	if len(a) == 0 || len(a) != len(b) {
		return 0, errors.New("vectors must have the same non-zero dimensions")
	}
	var dot, aa, bb float64
	for i := range a {
		dot += float64(a[i] * b[i])
		aa += float64(a[i] * a[i])
		bb += float64(b[i] * b[i])
	}
	if aa == 0 || bb == 0 {
		return 0, nil
	}
	return dot / math.Sqrt(aa*bb), nil
}
