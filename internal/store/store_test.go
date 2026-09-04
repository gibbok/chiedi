package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestLifecycleFTSAndDelete(t *testing.T){
	ctx:=context.Background();path:=filepath.Join(t.TempDir(),"index.db")
	s,err:=Open(ctx,path);if err!=nil{t.Fatal(err)}
	root:=t.TempDir();if err:=s.AddRoot(ctx,root);err!=nil{t.Fatal(err)};roots,_:=s.Roots(ctx)
	id,err:=s.ReplaceDocument(ctx,Replacement{RootID:roots[0].ID,RelativePath:"policy.md",Size:10,MtimeNS:1,Format:"md",Status:"indexed",Chunks:[]Chunk{{Ordinal:0,Text:"Reference ZX-481 grants annual leave.",EmbeddingHash:[]byte("hash"),Vector:[]float32{1,0}}}});if err!=nil{t.Fatal(err)}
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
