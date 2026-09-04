package retrieval

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gibbok/local-genius/internal/embedding"
	"github.com/gibbok/local-genius/internal/indexer"
	"github.com/gibbok/local-genius/internal/store"
)

type qualityCase struct{question,source,path string}

func TestFiftyCaseRetrievalQualityGate(t *testing.T){
	pairs:=[][2]string{
		{"automobile","car"},{"physician","doctor"},{"child","kid"},{"residence","home"},{"occupation","job"},
		{"defect","bug"},{"begin","start"},{"finish","complete"},{"secure","safe"},{"assist","help"},
		{"acquire","obtain"},{"require","need"},{"configure","setup"},{"remove","delete"},{"create","generate"},
		{"change","modify"},{"folder","directory"},{"repository","codebase"},{"search","locate"},{"salary","wage"},
		{"customer","client"},{"repair","fix"},{"incorrect","wrong"},{"permit","allow"},{"prohibit","forbid"},
		{"select","choose"},{"display","show"},{"conceal","hide"},{"authenticate","login"},{"credential","password"},
		{"execute","run"},{"stop","halt"},{"previous","prior"},{"next","following"},{"small","tiny"},
		{"large","huge"},{"message","notification"},{"network","internet"},{"database","storage"},{"purchase","buy"},
	}
	cases:=make([]qualityCase,0,50);for n,pair:=range pairs{cases=append(cases,qualityCase{question:"What does "+pair[0]+" mean here?",source:pair[1],path:fmt.Sprintf("semantic-%02d.txt",n)})};for n:=0;n<10;n++{reference:=fmt.Sprintf("QA-ZX-%04d",9000+n);cases=append(cases,qualityCase{question:reference,source:"Exact reference "+reference,path:fmt.Sprintf("identifier-%02d.txt",n)})};if len(cases)!=50{t.Fatalf("quality dataset has %d cases, want 50",len(cases))}
	ctx:=context.Background();root:=t.TempDir();for _,test:=range cases{if err:=os.WriteFile(filepath.Join(root,test.path),[]byte(test.source),0o600);err!=nil{t.Fatal(err)}};s,err:=store.Open(ctx,filepath.Join(t.TempDir(),"quality.db"));if err!=nil{t.Fatal(err)};defer s.Close();if err:=s.AddRoot(ctx,root);err!=nil{t.Fatal(err)};embedder:=embedding.Projection{};stats,err:=(indexer.Indexer{Store:s,Embedder:embedder}).Reconcile(ctx);if err!=nil{t.Fatal(err)};if stats.EmbeddingJobs!=len(cases){t.Fatalf("indexed %d embeddings, want %d",stats.EmbeddingJobs,len(cases))};retriever:=Retriever{Store:s,Embedder:embedder}
	for n,test:=range cases{t.Run(fmt.Sprintf("%02d_%s",n,test.path),func(t *testing.T){results,err:=retriever.Retrieve(ctx,test.question,"",3);if err!=nil{t.Fatal(err)};if len(results)==0||results[0].Path!=test.path{t.Fatalf("question %q: top results=%+v, want %s",test.question,results,test.path)}})}
}
