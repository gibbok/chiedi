package retrieval

import (
	"context"
	"math"
	"path/filepath"
	"testing"

	"github.com/gibbok/local-genius/internal/store"
)

type fixedEmbedder struct{vectors [][]float32;dimensions int}
func (e fixedEmbedder)ID()string{return "test"}
func (e fixedEmbedder)Dimensions()int{return e.dimensions}
func (e fixedEmbedder)Embed(context.Context,[]string)([][]float32,error){return e.vectors,nil}

func TestFTSQuery(t *testing.T){got:=ftsQuery(`Reference ZX-481`);if got!=`"reference" OR "zx" OR "481"`{t.Fatalf("got %q",got)}}

func TestRetrieveRejectsInvalidQueryEmbeddingOutput(t *testing.T){
	ctx:=context.Background();s,err:=store.Open(ctx,filepath.Join(t.TempDir(),"retrieve.db"));if err!=nil{t.Fatal(err)};defer s.Close()
	tests:=[]struct{name string;vectors [][]float32;dimensions int}{{"no vector",[][]float32{},2},{"too many vectors",[][]float32{{1,0},{1,0}},2},{"wrong dimensions",[][]float32{{1}},2},{"NaN",[][]float32{{float32(math.NaN()),0}},2},{"infinity",[][]float32{{float32(math.Inf(1)),0}},2}}
	for _,test:=range tests{t.Run(test.name,func(t *testing.T){_,err:=(Retriever{Store:s,Embedder:fixedEmbedder{test.vectors,test.dimensions}}).Retrieve(ctx,"question","",1);if err==nil{t.Fatal("expected invalid embedding output error")}})}
}
