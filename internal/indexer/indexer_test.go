package indexer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gibbok/local-genius/internal/embedding"
	"github.com/gibbok/local-genius/internal/store"
)

type incompatibleEmbedder struct{embedding.Projection}
func (incompatibleEmbedder) ID()string{return "different-model"}

func TestIncrementalReuseRenameDeleteAndFailure(t *testing.T){
	ctx:=context.Background();root:=t.TempDir();db:=filepath.Join(t.TempDir(),"index.db")
	policy:=filepath.Join(root,"policy.md");writeAt(t,policy,"# Policy\n\n## Leave\n\nEmployees receive twenty business days of paid annual leave.\n\n## Travel\n\nTrains are preferred.",time.Now())
	if err:=os.WriteFile(filepath.Join(root,"ignored.bin"),[]byte("ignore"),0o600);err!=nil{t.Fatal(err)}
	s,err:=store.Open(ctx,db);if err!=nil{t.Fatal(err)};defer s.Close();if err:=s.AddRoot(ctx,root);err!=nil{t.Fatal(err)}
	i:=Indexer{Store:s,Embedder:embedding.Projection{}}
	first,err:=i.Reconcile(ctx);if err!=nil{t.Fatal(err)};if first.EmbeddingJobs!=2||first.ExtractionJobs!=1{t.Fatalf("first=%+v",first)}
	second,err:=i.Reconcile(ctx);if err!=nil{t.Fatal(err)};if second.EmbeddingJobs!=0||second.ExtractionJobs!=0{t.Fatalf("unchanged=%+v",second)}
	writeAt(t,policy,"# Policy\n\n## Leave\n\nEmployees receive twenty-five business days of paid annual leave.\n\n## Travel\n\nTrains are preferred.",time.Now().Add(2*time.Second))
	changed,err:=i.Reconcile(ctx);if err!=nil{t.Fatal(err)};if changed.EmbeddingJobs!=1||changed.ReusedChunks!=1{t.Fatalf("changed=%+v",changed)}
	renamed:=filepath.Join(root,"people.md");if err:=os.Rename(policy,renamed);err!=nil{t.Fatal(err)}
	renameStats,err:=i.Reconcile(ctx);if err!=nil{t.Fatal(err)};if renameStats.Renamed!=1||renameStats.EmbeddingJobs!=0{t.Fatalf("rename=%+v",renameStats)}
	if err:=os.WriteFile(filepath.Join(root,"bad.pdf"),[]byte("not a PDF"),0o600);err!=nil{t.Fatal(err)}
	bad,err:=i.Reconcile(ctx);if err!=nil{t.Fatal(err)};if bad.Failed!=1{t.Fatalf("bad PDF=%+v",bad)}
	if err:=os.Remove(renamed);err!=nil{t.Fatal(err)}
	deleted,err:=i.Reconcile(ctx);if err!=nil{t.Fatal(err)};if deleted.Deleted!=1{t.Fatalf("delete=%+v",deleted)}
	docs,err:=s.ListDocuments(ctx,"","","indexed");if err!=nil{t.Fatal(err)};if len(docs)!=0{t.Fatalf("indexed docs remain: %+v",docs)}
}

func TestRejectsEmbeddingModelMismatch(t *testing.T){
	ctx:=context.Background();root:=t.TempDir();writeAt(t,filepath.Join(root,"doc.txt"),"semantic content",time.Now());s,err:=store.Open(ctx,filepath.Join(t.TempDir(),"index.db"));if err!=nil{t.Fatal(err)};defer s.Close();if err:=s.AddRoot(ctx,root);err!=nil{t.Fatal(err)}
	if _,err:=(Indexer{Store:s,Embedder:embedding.Projection{}}).Reconcile(ctx);err!=nil{t.Fatal(err)}
	if _,err:=(Indexer{Store:s,Embedder:incompatibleEmbedder{}}).Reconcile(ctx);err==nil{t.Fatal("expected embedding model mismatch") } else if got:=err.Error();got==""{t.Fatal(fmt.Errorf("empty mismatch error"))}
}

func writeAt(t *testing.T,path,content string,mtime time.Time){t.Helper();if err:=os.WriteFile(path,[]byte(content),0o600);err!=nil{t.Fatal(err)};if err:=os.Chtimes(path,mtime,mtime);err!=nil{t.Fatal(err)}}
