package store

import (
 "context"
 "database/sql"
 "fmt"
 "path/filepath"
 "strings"
 "testing"
)

func vectorStore(t *testing.T)(context.Context,*Store,Replacement){
 t.Helper();ctx:=context.Background();s,err:=Open(ctx,filepath.Join(t.TempDir(),"vec.db"));if err!=nil{t.Fatal(err)};t.Cleanup(func(){s.Close()})
 if err:=s.AddRoot(ctx,t.TempDir());err!=nil{t.Fatal(err)};roots,err:=s.Roots(ctx);if err!=nil{t.Fatal(err)}
 return ctx,s,Replacement{RootID:roots[0].ID,RelativePath:"doc.txt",Format:"txt",Status:"indexed",Chunks:[]Chunk{{Text:"evidence",EmbeddingHash:make([]byte,32),Vector:[]float32{1,0}}}}
}

func TestSQLiteVecLifecycleAndFiltering(t *testing.T){
 ctx,s,r:=vectorStore(t)
 var version string;if err:=s.db.QueryRow(`SELECT vec_version()`).Scan(&version);err!=nil||version==""{t.Fatalf("sqlite-vec: %q %v",version,err)}
 for n:=0;n<45;n++{r.RelativePath=fmt.Sprintf("outside/%02d.txt",n);if _,err:=s.ReplaceDocument(ctx,r);err!=nil{t.Fatal(err)}}
 r.RelativePath="inside/percent%.txt";r.Chunks[0].Vector=[]float32{.8,.2};id,err:=s.ReplaceDocument(ctx,r);if err!=nil{t.Fatal(err)}
 hits,err:=s.Candidates(ctx,[]float32{1,0},"nomatch","inside/percent%",1);if err!=nil||len(hits)!=1||hits[0].Chunk.DocumentID!=id||hits[0].VectorRank!=1{t.Fatalf("filtered KNN: %+v %v",hits,err)}
 if err:=s.RenameDocument(ctx,id,"renamed.txt","",0,0);err!=nil{t.Fatal(err)}
 hits,err=s.Candidates(ctx,[]float32{1,0},"nomatch","renamed",1);if err!=nil||len(hits)!=1{t.Fatalf("rename: %+v %v",hits,err)}
 if err:=s.DeleteDocument(ctx,id);err!=nil{t.Fatal(err)}
 hits,err=s.Candidates(ctx,[]float32{1,0},"nomatch","renamed",1);if err!=nil||len(hits)!=0{t.Fatalf("delete: %+v %v",hits,err)}
 roots,_:=s.Roots(ctx);if err:=s.RemoveRoot(ctx,roots[0].Path);err!=nil{t.Fatal(err)}
 if err:=s.IndexIntegrityCheck(ctx,2);err!=nil{t.Fatal(err)}
 var count int;if err:=s.db.QueryRow(`SELECT count(*) FROM chunks_vec`).Scan(&count);err!=nil||count!=0{t.Fatalf("orphan vectors: %d %v",count,err)}
}

func TestChunkIDsNeverAliasAfterReplacementAndDeletion(t *testing.T){
 ctx,s,r:=vectorStore(t);id,err:=s.ReplaceDocument(ctx,r);if err!=nil{t.Fatal(err)};old,_:=s.ChunksForDocument(ctx,id)
 r.Chunks[0].Text="changed evidence";if _,err:=s.ReplaceDocument(ctx,r);err!=nil{t.Fatal(err)}
 if _,err:=s.ReadChunks(ctx,[]int64{old[0].ID},0,0);err==nil||!strings.Contains(err.Error(),"stale"){t.Fatalf("old ID accepted: %v",err)}
 current,_:=s.ChunksForDocument(ctx,id);if current[0].ID<=old[0].ID{t.Fatal("ID reused")}
 if err:=s.DeleteDocument(ctx,id);err!=nil{t.Fatal(err)};id,err=s.ReplaceDocument(ctx,r);if err!=nil{t.Fatal(err)}
 newest,_:=s.ChunksForDocument(ctx,id);if newest[0].ID<=current[0].ID{t.Fatal("ID reused after deleting last row")}
}

func TestHybridCandidatesUseOneSnapshot(t *testing.T){
 ctx,s,r:=vectorStore(t);if _,err:=s.ReplaceDocument(ctx,r);err!=nil{t.Fatal(err)}
 writer,err:=Open(ctx,s.Path());if err!=nil{t.Fatal(err)};defer writer.Close()
 tx,err:=s.db.BeginTx(ctx,&sql.TxOptions{ReadOnly:true});if err!=nil{t.Fatal(err)};defer tx.Rollback()
 var count int;if err:=tx.QueryRow(`SELECT count(*) FROM chunks`).Scan(&count);err!=nil{t.Fatal(err)}
 r.RelativePath="new.txt";r.Chunks[0].Text="newneedle";if _,err:=writer.ReplaceDocument(ctx,r);err!=nil{t.Fatal(err)}
 old,err:=candidatesInSnapshot(ctx,tx,[]float32{1,0},"newneedle","",40);if err!=nil||len(old)!=1||old[0].Chunk.Path!="doc.txt"||old[0].LexicalRank!=0{t.Fatalf("mixed snapshot: %+v %v",old,err)}
 tx.Rollback()
 fresh,err:=s.Candidates(ctx,[]float32{1,0},"newneedle","",40);if err!=nil||len(fresh)!=2{t.Fatalf("fresh snapshot: %+v %v",fresh,err)}
 for _,hit:=range fresh{if hit.Chunk.ID==0||hit.Chunk.Text==""{t.Fatalf("empty evidence: %+v",hit)}}
}

func TestLegacyMigrationPreservesVectorsAndFTS(t *testing.T){
 ctx,s,r:=vectorStore(t);id,err:=s.ReplaceDocument(ctx,r);if err!=nil{t.Fatal(err)}
 // Reconstruct the previous schema: same columns, reusable INTEGER PRIMARY KEY,
 // no vector table. Preserve the original chunk/FTS identifiers.
 for _,q:=range []string{
 `DROP TABLE chunks_vec`, `DELETE FROM meta WHERE key='vector_dimensions'`,
 `CREATE TABLE legacy_chunks (id INTEGER PRIMARY KEY, document_id INTEGER NOT NULL, ordinal INTEGER NOT NULL,page_start INTEGER,page_end INTEGER,heading TEXT,text TEXT NOT NULL,embedding_hash BLOB NOT NULL,embedding BLOB NOT NULL,UNIQUE(document_id,ordinal),FOREIGN KEY(document_id) REFERENCES documents(id) ON DELETE CASCADE)`,
 `INSERT INTO legacy_chunks SELECT * FROM chunks`,`DROP TABLE chunks`,`ALTER TABLE legacy_chunks RENAME TO chunks`,`UPDATE meta SET value='1' WHERE key='schema_version'`,
 }{if _,err:=s.db.Exec(q);err!=nil{t.Fatal(err)}}
 path:=s.Path();s.Close();migrated,err:=Open(ctx,path);if err!=nil{t.Fatal(err)};defer migrated.Close()
 if err:=migrated.IndexIntegrityCheck(ctx,2);err!=nil{t.Fatal(err)}
 hits,err:=migrated.Candidates(ctx,[]float32{1,0},"evidence","",10);if err!=nil||len(hits)!=1||hits[0].Chunk.DocumentID!=id{t.Fatalf("migration lost evidence: %+v %v",hits,err)}
 old:=hits[0].Chunk.ID;if _,err:=migrated.ReplaceDocument(ctx,r);err!=nil{t.Fatal(err)}
 if _,err:=migrated.ReadChunks(ctx,[]int64{old},0,0);err==nil{t.Fatal("migration reused legacy ID")}
}

func TestVectorCorruptionAndDimensionMismatch(t *testing.T){
 ctx,s,r:=vectorStore(t);id,err:=s.ReplaceDocument(ctx,r);if err!=nil{t.Fatal(err)}
 r.Chunks[0].Vector=[]float32{1,0,0};if _,err:=s.ReplaceDocument(ctx,r);err==nil{t.Fatal("accepted incompatible dimensions")}
 if err:=s.IndexIntegrityCheck(ctx,2);err!=nil{t.Fatal(err)}
 chunks,_:=s.ChunksForDocument(ctx,id);if _,err:=s.db.Exec(`DELETE FROM chunks_vec WHERE rowid=?`,chunks[0].ID);err!=nil{t.Fatal(err)}
 if err:=s.IndexIntegrityCheck(ctx,2);err==nil{t.Fatal("missing vector undetected")}
}

func TestConcurrentDocumentWriters(t *testing.T){
 ctx,s,r:=vectorStore(t);other,err:=Open(ctx,s.Path());if err!=nil{t.Fatal(err)};defer other.Close()
 done:=make(chan error,2)
 for n,target:=range []*Store{s,other}{go func(n int,target *Store){for j:=0;j<8;j++{copy:=r;copy.RelativePath=fmt.Sprintf("writer-%d-%d.txt",n,j);if _,err:=target.ReplaceDocument(ctx,copy);err!=nil{done<-err;return}};done<-nil}(n,target)}
 for n:=0;n<2;n++{if err:=<-done;err!=nil{t.Fatal(err)}}
 counts,err:=s.Counts(ctx);if err!=nil||counts.Chunks!=16{t.Fatalf("lost writes: %+v %v",counts,err)}
 if err:=s.IndexIntegrityCheck(ctx,2);err!=nil{t.Fatal(err)}
}
