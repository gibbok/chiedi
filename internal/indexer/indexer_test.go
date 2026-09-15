package indexer

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gibbok/chiedi/internal/embedding"
	"github.com/gibbok/chiedi/internal/extract"
	"github.com/gibbok/chiedi/internal/store"
)

type incompatibleEmbedder struct{embedding.E5}
func (incompatibleEmbedder) ID()string{return "different-model"}
type invalidOutputEmbedder struct{embedding.E5}
func (invalidOutputEmbedder) Embed(context.Context,[]string)([][]float32,error){return [][]float32{},nil}
type nonFiniteOutputEmbedder struct{embedding.E5}
func (nonFiniteOutputEmbedder) Embed(_ context.Context,texts []string)([][]float32,error){out:=make([][]float32,len(texts));for n:=range out{out[n]=make([]float32,(embedding.E5{}).Dimensions());out[n][0]=float32(math.NaN())};return out,nil}
type cancelEmbedder struct{embedding.E5;started chan struct{}}
func (e cancelEmbedder)Embed(ctx context.Context,_ []string)([][]float32,error){close(e.started);<-ctx.Done();return nil,ctx.Err()}

func TestIncrementalReuseRenameDeleteAndFailure(t *testing.T){
	ctx:=context.Background();root:=t.TempDir();db:=filepath.Join(t.TempDir(),"index.db")
	policy:=filepath.Join(root,"policy.md");writeAt(t,policy,"# Policy\n\n## Leave\n\nEmployees receive twenty business days of paid annual leave.\n\n## Travel\n\nTrains are preferred.",time.Now())
	if err:=os.WriteFile(filepath.Join(root,"ignored.bin"),[]byte("ignore"),0o600);err!=nil{t.Fatal(err)}
	s,err:=store.Open(ctx,db);if err!=nil{t.Fatal(err)};defer s.Close();if err:=s.AddRoot(ctx,root);err!=nil{t.Fatal(err)}
	i:=Indexer{Store:s,Embedder:embedding.E5{}}
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
	stats,err:=(Indexer{Store:s,Embedder:embedding.E5{}}).Reconcile(ctx);if err!=nil{t.Fatal(err)};if stats.Failed!=1||stats.EmbeddingJobs!=0{t.Fatalf("stats=%+v",stats)}
	docs,err:=s.ListDocuments(ctx,"","","failed");if err!=nil{t.Fatal(err)};if len(docs)!=1{t.Fatalf("failed docs=%d",len(docs))}
}

func TestRejectsEmbeddingModelMismatch(t *testing.T){
	ctx:=context.Background();root:=t.TempDir();writeAt(t,filepath.Join(root,"doc.txt"),"semantic content",time.Now());s,err:=store.Open(ctx,filepath.Join(t.TempDir(),"index.db"));if err!=nil{t.Fatal(err)};defer s.Close();if err:=s.AddRoot(ctx,root);err!=nil{t.Fatal(err)}
	if _,err:=(Indexer{Store:s,Embedder:embedding.E5{}}).Reconcile(ctx);err!=nil{t.Fatal(err)}
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
	s,err:=store.Open(ctx,filepath.Join(t.TempDir(),"index.db"));if err!=nil{t.Fatal(err)};defer s.Close();if err:=s.AddRoot(ctx,root);err!=nil{t.Fatal(err)};i:=Indexer{Store:s,Embedder:embedding.E5{}};if _,err:=i.Reconcile(ctx);err!=nil{t.Fatal(err)};roots,err:=s.Roots(ctx);if err!=nil{t.Fatal(err)};before,err:=s.DocumentsByRoot(ctx,roots[0].ID);if err!=nil||len(before)!=1{t.Fatalf("before=%+v err=%v",before,err)}
	replacement:=filepath.Join(root,"replacement.tmp");replacementTime:=time.Now();writeAt(t,replacement,"unchanged semantic content",replacementTime);if err:=os.Remove(path);err!=nil{t.Fatal(err)};if err:=os.Rename(replacement,path);err!=nil{t.Fatal(err)};current,err:=os.Stat(path);if err!=nil{t.Fatal(err)}
	stats,err:=i.Reconcile(ctx);if err!=nil{t.Fatal(err)};if stats.ExtractionJobs!=0||stats.EmbeddingJobs!=0{t.Fatalf("content-equivalent replacement did unnecessary work: %+v",stats)};after,err:=s.DocumentsByRoot(ctx,roots[0].ID);if err!=nil||len(after)!=1{t.Fatalf("after=%+v err=%v",after,err)};if after[0].MtimeNS!=current.ModTime().UnixNano(){t.Fatalf("mtime was not refreshed: got %d want %d",after[0].MtimeNS,current.ModTime().UnixNano())};if before[0].Identity!=""&&after[0].Identity!=""&&before[0].Identity==after[0].Identity{t.Fatalf("file identity was not refreshed: %q",after[0].Identity)}
}

func TestReplacementWithSameSizeAndMtimeIsDetectedByIdentity(t *testing.T){
	ctx:=context.Background();root:=t.TempDir();path:=filepath.Join(root,"doc.txt");fixed:=time.Unix(1700000000,0);writeAt(t,path,"alpha",fixed)
	s,err:=store.Open(ctx,filepath.Join(t.TempDir(),"index.db"));if err!=nil{t.Fatal(err)};defer s.Close();if err:=s.AddRoot(ctx,root);err!=nil{t.Fatal(err)};i:=Indexer{Store:s,Embedder:embedding.E5{}};if _,err:=i.Reconcile(ctx);err!=nil{t.Fatal(err)};roots,err:=s.Roots(ctx);if err!=nil{t.Fatal(err)};before,err:=s.DocumentsByRoot(ctx,roots[0].ID);if err!=nil||len(before)!=1{t.Fatalf("before=%+v err=%v",before,err)}
	replacement:=filepath.Join(root,"replacement.tmp");writeAt(t,replacement,"bravo",fixed);replacementInfo,err:=os.Stat(replacement);if err!=nil{t.Fatal(err)};replacementIdentity:=fileIdentity(replacementInfo);if before[0].Identity==""||replacementIdentity==""||before[0].Identity==replacementIdentity{t.Skip("filesystem does not expose distinct file identities")};if err:=os.Remove(path);err!=nil{t.Fatal(err)};if err:=os.Rename(replacement,path);err!=nil{t.Fatal(err)}
	stats,err:=i.Reconcile(ctx);if err!=nil{t.Fatal(err)};if stats.ExtractionJobs!=1||stats.EmbeddingJobs!=1{t.Fatalf("same-metadata replacement was not reindexed: %+v",stats)};documents,err:=s.DocumentsByRoot(ctx,roots[0].ID);if err!=nil||len(documents)!=1{t.Fatalf("documents=%+v err=%v",documents,err)};if documents[0].Identity!=replacementIdentity{t.Fatalf("identity=%q want %q",documents[0].Identity,replacementIdentity)};chunks,err:=s.ChunksForDocument(ctx,documents[0].ID);if err!=nil||len(chunks)!=1||chunks[0].Text!="bravo"{t.Fatalf("replacement content not indexed: chunks=%+v err=%v",chunks,err)}
}

func TestRenameAcrossFormatsReextractsDocument(t *testing.T){
	ctx:=context.Background();root:=t.TempDir();textPath:=filepath.Join(root,"guide.txt");writeAt(t,textPath,"# Guide\n\nBody",time.Unix(1700000000,0));s,err:=store.Open(ctx,filepath.Join(t.TempDir(),"index.db"));if err!=nil{t.Fatal(err)};defer s.Close();if err:=s.AddRoot(ctx,root);err!=nil{t.Fatal(err)};i:=Indexer{Store:s,Embedder:embedding.E5{}};if _,err:=i.Reconcile(ctx);err!=nil{t.Fatal(err)}
	markdownPath:=filepath.Join(root,"guide.md");if err:=os.Rename(textPath,markdownPath);err!=nil{t.Fatal(err)};stats,err:=i.Reconcile(ctx);if err!=nil{t.Fatal(err)};if stats.Renamed!=1||stats.ExtractionJobs!=1{t.Fatalf("cross-format rename was treated as metadata-only: %+v",stats)};documents,err:=s.ListDocuments(ctx,"","","");if err!=nil||len(documents)!=1{t.Fatalf("documents=%+v err=%v",documents,err)};if documents[0].RelativePath!="guide.md"||documents[0].Format!="md"{t.Fatalf("format metadata is stale: %+v",documents[0])};chunks,err:=s.ChunksForDocument(ctx,documents[0].ID);if err!=nil||len(chunks)!=1{t.Fatalf("chunks=%+v err=%v",chunks,err)};if chunks[0].Heading!="Guide"||chunks[0].Text!="Body"{t.Fatalf("Markdown provenance was not regenerated: %+v",chunks[0])}
}

func TestCancellationDuringEmbeddingPreservesPreviousDocument(t *testing.T){
	ctx:=context.Background();root:=t.TempDir();path:=filepath.Join(root,"doc.txt");writeAt(t,path,"old durable content",time.Now().Add(-2*time.Second));dbPath:=filepath.Join(t.TempDir(),"cancel.db");s,err:=store.Open(ctx,dbPath);if err!=nil{t.Fatal(err)};defer s.Close();if err:=s.AddRoot(ctx,root);err!=nil{t.Fatal(err)};if _,err:=(Indexer{Store:s,Embedder:embedding.E5{}}).Reconcile(ctx);err!=nil{t.Fatal(err)};writeAt(t,path,"new interrupted content",time.Now())
	started:=make(chan struct{});cancelCtx,cancel:=context.WithCancel(ctx);done:=make(chan error,1);go func(){_,err:=(Indexer{Store:s,Embedder:cancelEmbedder{E5:embedding.E5{},started:started}}).Reconcile(cancelCtx);done<-err}();select{case <-started:cancel();case <-time.After(2*time.Second):t.Fatal("embedding did not start")};select{case err:=<-done:if !errors.Is(err,context.Canceled){t.Fatalf("got %v",err)};case <-time.After(2*time.Second):t.Fatal("reconciliation did not stop after cancellation")}
	documents,err:=s.ListDocuments(ctx,"","","");if err!=nil||len(documents)!=1{t.Fatalf("documents=%+v err=%v",documents,err)};chunks,err:=s.ChunksForDocument(ctx,documents[0].ID);if err!=nil||len(chunks)!=1||chunks[0].Text!="old durable content"{t.Fatalf("cancelled replacement damaged prior data: chunks=%+v err=%v",chunks,err)};if err:=s.IndexIntegrityCheck(ctx,(embedding.E5{}).Dimensions());err!=nil{t.Fatal(err)}
}

func TestUnavailableRootDoesNotDeleteIndexedData(t *testing.T){
	ctx:=context.Background();parent:=t.TempDir();root:=filepath.Join(parent,"corpus");if err:=os.Mkdir(root,0o700);err!=nil{t.Fatal(err)};writeAt(t,filepath.Join(root,"doc.txt"),"retained evidence",time.Now());s,err:=store.Open(ctx,filepath.Join(t.TempDir(),"offline.db"));if err!=nil{t.Fatal(err)};defer s.Close();if err:=s.AddRoot(ctx,root);err!=nil{t.Fatal(err)};i:=Indexer{Store:s,Embedder:embedding.E5{}};if _,err:=i.Reconcile(ctx);err!=nil{t.Fatal(err)};offline:=filepath.Join(parent,"corpus-offline");if err:=os.Rename(root,offline);err!=nil{t.Fatal(err)};stats,err:=i.Reconcile(ctx);if err==nil{t.Fatal("expected unavailable-root error")};if stats.Deleted!=0{t.Fatalf("unavailable root deleted documents: %+v",stats)};counts,countErr:=s.Counts(ctx);if countErr!=nil{t.Fatal(countErr)};if counts.Documents!=1||counts.Chunks!=1{t.Fatalf("unavailable root lost index data: %+v",counts)};ids,searchErr:=s.SearchFTS(ctx,"retained","",10);if searchErr!=nil||len(ids)!=1{t.Fatalf("retained evidence unavailable: ids=%v err=%v",ids,searchErr)}
}

func TestSymlinksCannotEscapeConfiguredRoot(t *testing.T){
	ctx:=context.Background();root:=t.TempDir();outside:=t.TempDir();writeAt(t,filepath.Join(root,"local.txt"),"local evidence",time.Now());writeAt(t,filepath.Join(outside,"secret.txt"),"external secret marker",time.Now());if err:=os.Symlink(outside,filepath.Join(root,"linked-directory"));err!=nil{t.Skipf("directory symlinks unavailable: %v",err)};if err:=os.Symlink(filepath.Join(outside,"secret.txt"),filepath.Join(root,"linked-file.txt"));err!=nil{t.Skipf("file symlinks unavailable: %v",err)}
	s,err:=store.Open(ctx,filepath.Join(t.TempDir(),"symlink.db"));if err!=nil{t.Fatal(err)};defer s.Close();if err:=s.AddRoot(ctx,root);err!=nil{t.Fatal(err)};stats,err:=(Indexer{Store:s,Embedder:embedding.E5{}}).Reconcile(ctx);if err!=nil{t.Fatal(err)};if stats.ScannedFiles!=1{t.Fatalf("symlink targets were scanned: %+v",stats)};documents,err:=s.ListDocuments(ctx,"","","");if err!=nil||len(documents)!=1||documents[0].RelativePath!="local.txt"{t.Fatalf("unexpected indexed documents: %+v err=%v",documents,err)};ids,err:=s.SearchFTS(ctx,"external","",10);if err!=nil||len(ids)!=0{t.Fatalf("external symlink content was indexed: ids=%v err=%v",ids,err)}
}

func writeAt(t *testing.T,path,content string,mtime time.Time){t.Helper();if err:=os.WriteFile(path,[]byte(content),0o600);err!=nil{t.Fatal(err)};if err:=os.Chtimes(path,mtime,mtime);err!=nil{t.Fatal(err)}}
