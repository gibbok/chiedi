package embedding

import (
	"context"
	"testing"
)

func TestProjectionSemanticSimilarity(t *testing.T) {
	e := Projection{}
	vs, err := e.Embed(context.Background(), []string{
		"How many vacation days do workers get?",
		"Employees are entitled to twenty business days of paid annual leave each year.",
		"The database uses write-ahead logging for durable transactions.",
	})
	if err != nil {
		t.Fatal(err)
	}
	relevant, _ := Cosine(vs[0], vs[1])
	distractor, _ := Cosine(vs[0], vs[2])
	if relevant <= distractor {
		t.Fatalf("semantic result=%f must outrank distractor=%f", relevant, distractor)
	}
	if len(vs[0]) != e.Dimensions() {
		t.Fatalf("got %d dimensions", len(vs[0]))
	}
}

func TestProjectionCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Projection{}).Embed(ctx, []string{"text"}); err == nil {
		t.Fatal("expected cancellation")
	}
}
