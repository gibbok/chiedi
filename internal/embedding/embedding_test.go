package embedding

import (
	"context"
	"math"
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

func TestProjectionConceptFeaturesResistSignedHashCollisions(t *testing.T) {
	tests := []struct {
		name       string
		question   string
		relevant   string
		distractor string
	}{
		{name: "secure", question: "What does secure mean here?", relevant: "safe", distractor: "choose"},
		{name: "change", question: "What does change mean here?", relevant: "modify", distractor: "home"},
		{name: "display", question: "What does display mean here?", relevant: "show", distractor: "fix"},
	}

	embedder := Projection{}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			vectors, err := embedder.Embed(context.Background(), []string{
				test.question,
				"Document: relevant.txt\nSection:\n\n" + test.relevant,
				"Document: distractor.txt\nSection:\n\n" + test.distractor,
			})
			if err != nil {
				t.Fatal(err)
			}

			relevant, err := Cosine(vectors[0], vectors[1])
			if err != nil {
				t.Fatal(err)
			}
			distractor, err := Cosine(vectors[0], vectors[2])
			if err != nil {
				t.Fatal(err)
			}
			if relevant <= 0 || relevant <= distractor {
				t.Fatalf("relevant similarity=%f must be positive and outrank distractor=%f", relevant, distractor)
			}
		})
	}
}

func TestCosineRejectsNonFiniteValues(t *testing.T){
	for _,value:=range []float32{float32(math.NaN()),float32(math.Inf(1)),float32(math.Inf(-1))}{if _,err:=Cosine([]float32{value,1},[]float32{1,1});err==nil{t.Fatalf("accepted non-finite value %v",value)}}
}

func TestCosineHandlesLargeFiniteValues(t *testing.T){
	value:=float32(math.MaxFloat32);similarity,err:=Cosine([]float32{value,value},[]float32{value,value});if err!=nil{t.Fatal(err)};if math.Abs(similarity-1)>1e-12{t.Fatalf("similarity=%v",similarity)}
}

func TestProjectionCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Projection{}).Embed(ctx, []string{"text"}); err == nil {
		t.Fatal("expected cancellation")
	}
}
