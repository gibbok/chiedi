package indexer

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gibbok/local-genius/internal/embedding"
	"github.com/gibbok/local-genius/internal/extract"
	"github.com/gibbok/local-genius/internal/store"
)

type incompatibleEmbedder struct{embedding.Projection}
func (incompatibleEmbedder) ID()string{return "different-model"}
type invalidOutputEmbedder struct{embedding.Projection}
func (invalidOutputEmbedder) Embed(context.Context,[]string)([][]float32,error){return [][]float32{},nil}
type nonFiniteOutputEmbedder struct{embedding.Projection}
func (nonFiniteOutputEmbedder) Embed(_ context.Context,texts []string)([][]float32,error){out:=make([][]float32,len(texts));for n:=range out{out[n]=make([]float32,(embedding.Projection{}).Dimensions());out[n][0]=float32(math.NaN())};return out,nil}

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

func TestOversizedFileIsRejectedBeforeReading(t *testing.T){
	ctx:=context.Background();root:=t.TempDir();path:=filepath.Join(root,"huge.txt");file,err:=os.Create(path);if err!=nil{t.Fatal(err)};if err:=file.Truncate(extract.MaxFileBytes+1);err!=nil{file.Close();t.Fatal(err)};file.Close()
	s,err:=store.Open(ctx,filepath.Join(t.TempDir(),"index.db"));if err!=nil{t.Fatal(err)};defer s.Close();if err:=s.AddRoot(ctx,root);err!=nil{t.Fatal(err)}
	stats,err:=(Indexer{Store:s,Embedder:embedding.Projection{}}).Reconcile(ctx);if err!=nil{t.Fatal(err)};if stats.Failed!=1||stats.EmbeddingJobs!=0{t.Fatalf("stats=%+v",stats)}
	docs,err:=s.ListDocuments(ctx,"","","failed");if err!=nil{t.Fatal(err)};if len(docs)!=1{t.Fatalf("failed docs=%d",len(docs))}
}

func TestRejectsEmbeddingModelMismatch(t *testing.T){
	ctx:=context.Background();root:=t.TempDir();writeAt(t,filepath.Join(root,"doc.txt"),"semantic content",time.Now());s,err:=store.Open(ctx,filepath.Join(t.TempDir(),"index.db"));if err!=nil{t.Fatal(err)};defer s.Close();if err:=s.AddRoot(ctx,root);err!=nil{t.Fatal(err)}
	if _,err:=(Indexer{Store:s,Embedder:embedding.Projection{}}).Reconcile(ctx);err!=nil{t.Fatal(err)}
	if _,err:=(Indexer{Store:s,Embedder:incompatibleEmbedder{}}).Reconcile(ctx);err==nil{t.Fatal("expected embedding model mismatch") } else if got:=err.Error();got==""{t.Fatal(fmt.Errorf("empty mismatch error"))}
}

func TestRejectsInvalidEmbeddingOutput(t *testing.T){
	ctx:=context.Background();root:=t.TempDir();writeAt(t,filepath.Join(root,"doc.txt"),"semantic content",time.Now());s,err:=store.Open(ctx,filepath.Join(t.TempDir(),"index.db"));if err!=nil{t.Fatal(err)};defer s.Close();if err:=s.AddRoot(ctx,root);err!=nil{t.Fatal(err)}
	if _,err:=(Indexer{Store:s,Embedder:invalidOutputEmbedder{}}).Reconcile(ctx);err==nil{t.Fatal("expected invalid embedding output error")}
	counts,err:=s.Counts(ctx);if err!=nil{t.Fatal(err)};if counts.Documents!=0||counts.Chunks!=0{t.Fatalf("partial index was persisted: %+v",counts)}
}

func TestRejectsNonFiniteEmbeddingOutput(t *testing.T){
	ctx:=context.Background();root:=t.TempDir();writeAt(t,filepath.Join(root,"doc.txt"),"semantic content",time.Now());s,err:=store.Open(ctx,filepath.Join(t.TempDir(),"index.db"));if err!=nil{t.Fatal(err)};defer s.Close();if err:=s.AddRoot(ctx,root);err!=nil{t.Fatal(err)}
	if _,err:=(Indexer{Store:s,Embedder:nonFiniteOutputEmbedder{}}).Reconcile(ctx);err==nil{t.Fatal("expected non-finite embedding output error")}
	counts,err:=s.Counts(ctx);if err!=nil{t.Fatal(err)};if counts.Documents!=0||counts.Chunks!=0{t.Fatalf("partial index was persisted: %+v",counts)}
}

func TestContentEquivalentReplacementRefreshesFileMetadata(t *testing.T){
	ctx:=context.Background();root:=t.TempDir();path:=filepath.Join(root,"doc.txt");initialTime:=time.Now().Add(-4*time.Second);writeAt(t,path,"unchanged semantic content",initialTime)
	s,err:=store.Open(ctx,filepath.Join(t.TempDir(),"index.db"));if err!=nil{t.Fatal(err)};defer s.Close();if err:=s.AddRoot(ctx,root);err!=nil{t.Fatal(err)};i:=Indexer{Store:s,Embedder:embedding.Projection{}};if _,err:=i.Reconcile(ctx);err!=nil{t.Fatal(err)};roots,err:=s.Roots(ctx);if err!=nil{t.Fatal(err)};before,err:=s.DocumentsByRoot(ctx,roots[0].ID);if err!=nil||len(before)!=1{t.Fatalf("before=%+v err=%v",before,err)}
	replacement:=filepath.Join(root,"replacement.tmp");replacementTime:=time.Now();writeAt(t,replacement,"unchanged semantic content",replacementTime);if err:=os.Remove(path);err!=nil{t.Fatal(err)};if err:=os.Rename(replacement,path);err!=nil{t.Fatal(err)};current,err:=os.Stat(path);if err!=nil{t.Fatal(err)}
	stats,err:=i.Reconcile(ctx);if err!=nil{t.Fatal(err)};if stats.ExtractionJobs!=0||stats.EmbeddingJobs!=0{t.Fatalf("content-equivalent replacement did unnecessary work: %+v",stats)};after,err:=s.DocumentsByRoot(ctx,roots[0].ID);if err!=nil||len(after)!=1{t.Fatalf("after=%+v err=%v",after,err)};if after[0].MtimeNS!=current.ModTime().UnixNano(){t.Fatalf("mtime was not refreshed: got %d want %d",after[0].MtimeNS,current.ModTime().UnixNano())};if before[0].Identity!=""&&after[0].Identity!=""&&before[0].Identity==after[0].Identity{t.Fatalf("file identity was not refreshed: %q",after[0].Identity)}
}

func writeAt(t *testing.T,path,content string,mtime time.Time){t.Helper();if err:=os.WriteFile(path,[]byte(content),0o600);err!=nil{t.Fatal(err)};if err:=os.Chtimes(path,mtime,mtime);err!=nil{t.Fatal(err)}}
