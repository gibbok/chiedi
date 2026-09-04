package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLifecycleFTSAndDelete(t *testing.T){
	ctx:=context.Background();path:=filepath.Join(t.TempDir(),"index.db")
	s,err:=Open(ctx,path);if err!=nil{t.Fatal(err)}
	root:=t.TempDir();if err:=s.AddRoot(ctx,root);err!=nil{t.Fatal(err)};roots,_:=s.Roots(ctx)
	id,err:=s.ReplaceDocument(ctx,Replacement{RootID:roots[0].ID,RelativePath:"policy.md",Size:10,MtimeNS:1,Format:"md",Status:"indexed",Chunks:[]Chunk{{Ordinal:0,Text:"Reference ZX-481 grants annual leave.",EmbeddingHash:make([]byte,32),Vector:[]float32{1,0}}}});if err!=nil{t.Fatal(err)}
	if got,err:=s.SearchFTS(ctx,"ZX", "",10);err!=nil||len(got)!=1{t.Fatalf("FTS got=%v err=%v",got,err)}
	if err:=s.Close();err!=nil{t.Fatal(err)}
	s,err=Open(ctx,path);if err!=nil{t.Fatal(err)};defer s.Close()
	counts,err:=s.Counts(ctx);if err!=nil||counts.Chunks!=1{t.Fatalf("counts=%+v err=%v",counts,err)}
	if err:=s.DeleteDocument(ctx,id);err!=nil{t.Fatal(err)}
	counts,_=s.Counts(ctx);if counts.Documents!=0||counts.Chunks!=0{t.Fatalf("stale data: %+v",counts)}
}

func TestRejectsNewerSchema(t *testing.T){
	ctx:=context.Background();path:=filepath.Join(t.TempDir(),"future.db")
	db,err:=sql.Open("sqlite",path);if err!=nil{t.Fatal(err)}
	if _,err:=db.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL); INSERT INTO meta(key,value) VALUES('schema_version','999')`);err!=nil{t.Fatal(err)};db.Close()
	_,err=Open(ctx,path);if err==nil||!strings.Contains(err.Error(),"newer than supported"){t.Fatalf("expected newer-schema error, got %v",err)}
}

func TestDatabaseUsesOwnerOnlyPermissions(t *testing.T){
	if runtime.GOOS=="windows"{t.Skip("POSIX permission bits are not available")};ctx:=context.Background();path:=filepath.Join(t.TempDir(),"private.db");if err:=os.WriteFile(path,nil,0o666);err!=nil{t.Fatal(err)};if err:=os.Chmod(path,0o666);err!=nil{t.Fatal(err)};s,err:=Open(ctx,path);if err!=nil{t.Fatal(err)};defer s.Close();info,err:=os.Stat(path);if err!=nil{t.Fatal(err)};if info.Mode().Perm()!=0o600{t.Fatalf("database permissions are %04o; want 0600",info.Mode().Perm())};if wal,err:=os.Stat(path+"-wal");err==nil&&wal.Mode().Perm()&0o077!=0{t.Fatalf("WAL permissions are %04o; group/world access must be disabled",wal.Mode().Perm())}
}

func TestEmptyCollectionsEncodeAsArrays(t *testing.T){
	ctx:=context.Background();s,err:=Open(ctx,filepath.Join(t.TempDir(),"empty.db"));if err!=nil{t.Fatal(err)};defer s.Close()
	values:=[]any{}
	roots,err:=s.Roots(ctx);if err!=nil{t.Fatal(err)};values=append(values,roots)
	documents,err:=s.ListDocuments(ctx,"","","");if err!=nil{t.Fatal(err)};values=append(values,documents)
	chunks,err:=s.AllChunks(ctx,"");if err!=nil{t.Fatal(err)};values=append(values,chunks)
	read,err:=s.ReadChunks(ctx,[]int64{999},0,0);if err!=nil{t.Fatal(err)};values=append(values,read)
	ids,err:=s.SearchFTS(ctx," ","",10);if err!=nil{t.Fatal(err)};values=append(values,ids)
	for n,value:=range values{encoded,err:=json.Marshal(value);if err!=nil{t.Fatal(err)};if string(encoded)!="[]"{t.Fatalf("collection %d encoded as %s",n,encoded)}}
}

func TestPathPrefixesAreLiteralAndCaseSensitive(t *testing.T){
	ctx:=context.Background();s,err:=Open(ctx,filepath.Join(t.TempDir(),"prefix.db"));if err!=nil{t.Fatal(err)};defer s.Close();root:=t.TempDir();if err:=s.AddRoot(ctx,root);err!=nil{t.Fatal(err)};roots,err:=s.Roots(ctx);if err!=nil{t.Fatal(err)}
	for _,path:=range []string{"under_score.md","underXscore.md","percent%.md","percentX.md","Case.md","case.md"}{if _,err:=s.ReplaceDocument(ctx,Replacement{RootID:roots[0].ID,RelativePath:path,Size:1,MtimeNS:1,ContentHash:[]byte(path),Format:"md",Status:"indexed",Chunks:[]Chunk{{Text:"shared token",EmbeddingHash:make([]byte,32),Vector:[]float32{1,0}}}});err!=nil{t.Fatal(err)}}
	assertOnly:=func(prefix,want string){t.Helper();docs,err:=s.ListDocuments(ctx,prefix,"","");if err!=nil{t.Fatal(err)};if len(docs)!=1||docs[0].RelativePath!=want{t.Fatalf("prefix %q: got %+v",prefix,docs)};ids,err:=s.SearchFTS(ctx,"token",prefix,10);if err!=nil{t.Fatal(err)};if len(ids)!=1{t.Fatalf("FTS prefix %q returned %d IDs",prefix,len(ids))}}
	assertOnly("under_","under_score.md")
	assertOnly("percent%","percent%.md")
	assertOnly("Case","Case.md")
}

func TestMultiRootProvenanceDistinguishesEqualRelativePaths(t *testing.T){
	ctx:=context.Background();s,err:=Open(ctx,filepath.Join(t.TempDir(),"roots.db"));if err!=nil{t.Fatal(err)};defer s.Close();first,second:=t.TempDir(),t.TempDir();if err:=s.AddRoot(ctx,first);err!=nil{t.Fatal(err)};if err:=s.AddRoot(ctx,second);err!=nil{t.Fatal(err)};roots,err:=s.Roots(ctx);if err!=nil{t.Fatal(err)}
	for _,root:=range roots{if _,err:=s.ReplaceDocument(ctx,Replacement{RootID:root.ID,RelativePath:"same.md",Size:1,MtimeNS:1,Format:"md",Status:"indexed",Chunks:[]Chunk{{Text:root.Path,EmbeddingHash:make([]byte,32),Vector:[]float32{1,0}}}});err!=nil{t.Fatal(err)}}
	docs,err:=s.ListDocuments(ctx,"same","","");if err!=nil{t.Fatal(err)};if len(docs)!=2||docs[0].RootPath==docs[1].RootPath||docs[0].RootID==docs[1].RootID{t.Fatalf("document provenance is ambiguous: %+v",docs)}
	chunks,err:=s.AllChunks(ctx,"same");if err!=nil{t.Fatal(err)};if len(chunks)!=2||chunks[0].RootPath==chunks[1].RootPath||chunks[0].RootID==chunks[1].RootID{t.Fatalf("chunk provenance is ambiguous: %+v",chunks)}
}

func TestIndexIntegrityDetectsSemanticIndexCorruption(t *testing.T){
	ctx:=context.Background();s,err:=Open(ctx,filepath.Join(t.TempDir(),"integrity.db"));if err!=nil{t.Fatal(err)};defer s.Close();root:=t.TempDir();if err:=s.AddRoot(ctx,root);err!=nil{t.Fatal(err)};roots,err:=s.Roots(ctx);if err!=nil{t.Fatal(err)}
	_,err=s.ReplaceDocument(ctx,Replacement{RootID:roots[0].ID,RelativePath:"doc.md",Size:1,MtimeNS:1,Format:"md",Status:"indexed",Chunks:[]Chunk{{Heading:"Heading",Text:"body",EmbeddingHash:make([]byte,32),Vector:[]float32{1,0}}}});if err!=nil{t.Fatal(err)}
	chunks,err:=s.AllChunks(ctx,"");if err!=nil||len(chunks)!=1{t.Fatalf("chunks=%+v err=%v",chunks,err)};id:=chunks[0].ID
	if err:=s.IndexIntegrityCheck(ctx,2);err!=nil{t.Fatal(err)}
	if _,err:=s.db.ExecContext(ctx,`DELETE FROM chunks_fts WHERE rowid=?`,id);err!=nil{t.Fatal(err)};assertIntegrityError(t,s,ctx,2,"missing FTS")
	if _,err:=s.db.ExecContext(ctx,`INSERT INTO chunks_fts(rowid,text,heading,path) VALUES(?,?,?,?)`,id,"body","Heading","doc.md");err!=nil{t.Fatal(err)}
	if _,err:=s.db.ExecContext(ctx,`UPDATE chunks SET embedding_hash=? WHERE id=?`,[]byte{1},id);err!=nil{t.Fatal(err)};assertIntegrityError(t,s,ctx,2,"embedding hashes")
	if _,err:=s.db.ExecContext(ctx,`UPDATE chunks SET embedding_hash=?,embedding=? WHERE id=?`,make([]byte,32),[]byte{0,0,0,0},id);err!=nil{t.Fatal(err)};assertIntegrityError(t,s,ctx,2,"vector blobs")
	if _,err:=s.db.ExecContext(ctx,`UPDATE chunks SET embedding=? WHERE id=?`,encodeVector([]float32{float32(math.NaN()),1}),id);err!=nil{t.Fatal(err)};assertIntegrityError(t,s,ctx,2,"non-finite")
	if _,err:=s.db.ExecContext(ctx,`UPDATE chunks SET embedding=? WHERE id=?`,encodeVector([]float32{1,0}),id);err!=nil{t.Fatal(err)}
	if _,err:=s.db.ExecContext(ctx,`DELETE FROM chunks_fts WHERE rowid=?`,id);err!=nil{t.Fatal(err)};if _,err:=s.db.ExecContext(ctx,`INSERT INTO chunks_fts(rowid,text,heading,path) VALUES(?,?,?,?)`,id,"body","Heading","wrong.md");err!=nil{t.Fatal(err)};assertIntegrityError(t,s,ctx,2,"inconsistent")
}

func TestReplacementFailureRollsBackDocumentFTSAndVectors(t *testing.T){
	ctx:=context.Background();path:=filepath.Join(t.TempDir(),"rollback.db");s,err:=Open(ctx,path);if err!=nil{t.Fatal(err)};root:=t.TempDir();if err:=s.AddRoot(ctx,root);err!=nil{t.Fatal(err)};roots,err:=s.Roots(ctx);if err!=nil{t.Fatal(err)};replacement:=Replacement{RootID:roots[0].ID,RelativePath:"doc.txt",Size:3,MtimeNS:1,ContentHash:[]byte("old"),Format:"txt",Status:"indexed",Chunks:[]Chunk{{Ordinal:0,Text:"durable old evidence",EmbeddingHash:make([]byte,32),Vector:[]float32{1,0}}}};documentID,err:=s.ReplaceDocument(ctx,replacement);if err!=nil{t.Fatal(err)}
	broken:=Replacement{RootID:roots[0].ID,RelativePath:"doc.txt",Size:3,MtimeNS:2,ContentHash:[]byte("new"),Format:"txt",Status:"indexed",Chunks:[]Chunk{{Ordinal:0,Text:"partial replacement",EmbeddingHash:make([]byte,32),Vector:[]float32{0,1}},{Ordinal:0,Text:"duplicate ordinal",EmbeddingHash:make([]byte,32),Vector:[]float32{0,1}}}};if _,err:=s.ReplaceDocument(ctx,broken);err==nil{t.Fatal("expected replacement constraint failure")}
	chunks,err:=s.ChunksForDocument(ctx,documentID);if err!=nil||len(chunks)!=1||chunks[0].Text!="durable old evidence"{t.Fatalf("rollback lost original chunks: %+v err=%v",chunks,err)};oldIDs,err:=s.SearchFTS(ctx,"durable","",10);if err!=nil||len(oldIDs)!=1{t.Fatalf("original FTS row missing: ids=%v err=%v",oldIDs,err)};newIDs,err:=s.SearchFTS(ctx,"replacement","",10);if err!=nil||len(newIDs)!=0{t.Fatalf("partial FTS row survived: ids=%v err=%v",newIDs,err)};if err:=s.IndexIntegrityCheck(ctx,2);err!=nil{t.Fatal(err)};if err:=s.Close();err!=nil{t.Fatal(err)}
	s,err=Open(ctx,path);if err!=nil{t.Fatal(err)};defer s.Close();chunks,err=s.ChunksForDocument(ctx,documentID);if err!=nil||len(chunks)!=1||chunks[0].Text!="durable old evidence"{t.Fatalf("restart after rollback lost original: %+v err=%v",chunks,err)}
}

func TestConcurrentStoresWaitForWriterAndPreserveWrites(t *testing.T){
	ctx:=context.Background();path:=filepath.Join(t.TempDir(),"contention.db");first,err:=Open(ctx,path);if err!=nil{t.Fatal(err)};defer first.Close();second,err:=Open(ctx,path);if err!=nil{t.Fatal(err)};defer second.Close();tx,err:=first.db.BeginTx(ctx,nil);if err!=nil{t.Fatal(err)};if _,err:=tx.ExecContext(ctx,`INSERT INTO meta(key,value) VALUES('held_write','first')`);err!=nil{tx.Rollback();t.Fatal(err)}
	done:=make(chan error,1);go func(){done<-second.SetMeta(ctx,"concurrent_write","second")}();select{case err:=<-done:tx.Rollback();t.Fatalf("concurrent writer returned before lock release: %v",err);case <-time.After(100*time.Millisecond):};if err:=tx.Commit();err!=nil{t.Fatal(err)};select{case err:=<-done:if err!=nil{t.Fatal(err)};case <-time.After(3*time.Second):t.Fatal("concurrent writer did not resume after lock release")}
	var wg sync.WaitGroup;errors:=make(chan error,20);for n:=0;n<20;n++{wg.Add(1);go func(n int){defer wg.Done();target:=first;if n%2==1{target=second};errors<-target.SetMeta(ctx,fmt.Sprintf("parallel_%02d",n),fmt.Sprint(n))}(n)};wg.Wait();close(errors);for err:=range errors{if err!=nil{t.Fatal(err)}};for n:=0;n<20;n++{value,err:=first.Meta(ctx,fmt.Sprintf("parallel_%02d",n));if err!=nil||value!=fmt.Sprint(n){t.Fatalf("parallel write %d: value=%q err=%v",n,value,err)}}
}

func assertIntegrityError(t *testing.T,s *Store,ctx context.Context,dimensions int,want string){t.Helper();err:=s.IndexIntegrityCheck(ctx,dimensions);if err==nil||!strings.Contains(err.Error(),want){t.Fatalf("expected integrity error containing %q, got %v",want,err)}}
