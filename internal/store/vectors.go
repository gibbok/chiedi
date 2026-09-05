package store

import (
 "context"
 "database/sql"
 "fmt"
 "math"
 "strconv"
 "strings"
 "time"
)

type sqlReader interface {
 QueryContext(context.Context,string,...any)(*sql.Rows,error)
 QueryRowContext(context.Context,string,...any)*sql.Row
}

func validateVector(v []float32) error {
 if len(v)==0 { return fmt.Errorf("vector must have non-zero dimensions") }
 for _,x:=range v { if math.IsNaN(float64(x))||math.IsInf(float64(x),0) { return fmt.Errorf("vector must contain only finite values") } }
 return nil
}

// The dimensions are fixed for the lifetime of this database. The production
// embedder uses 384; keeping the schema parameterized also supports future models.
func ensureVectors(ctx context.Context,tx *sql.Tx,dimensions int) error {
 if dimensions<=0||dimensions>8192 { return fmt.Errorf("invalid vector dimensions: %d",dimensions) }
 var existing string
 err:=tx.QueryRowContext(ctx,`SELECT value FROM meta WHERE key='vector_dimensions'`).Scan(&existing)
 if err!=nil&&err!=sql.ErrNoRows { return err }
 if err==nil { if existing!=strconv.Itoa(dimensions) { return fmt.Errorf("vector dimensions: index uses %s, received %d",existing,dimensions) }; return nil }
 if _,err:=tx.ExecContext(ctx,fmt.Sprintf(`CREATE VIRTUAL TABLE chunks_vec USING vec0(embedding float[%d] distance_metric=cosine)`,dimensions));err!=nil{return err}
 _,err=tx.ExecContext(ctx,`INSERT INTO meta(key,value) VALUES('vector_dimensions',?)`,strconv.Itoa(dimensions))
 return err
}

func deleteVector(ctx context.Context,tx *sql.Tx,id int64) error {
 var count int
 if err:=tx.QueryRowContext(ctx,`SELECT count(*) FROM meta WHERE key='vector_dimensions'`).Scan(&count);err!=nil{return err}
 if count==0{return nil}
 _,err:=tx.ExecContext(ctx,`DELETE FROM chunks_vec WHERE rowid=?`,id)
 return err
}

func (s *Store) migrateVectors(ctx context.Context,previous string) error {
 var version string
 if err:=s.db.QueryRowContext(ctx,`SELECT vec_version()`).Scan(&version);err!=nil{return fmt.Errorf("sqlite-vec unavailable: %w",err)}
 if previous!="1" { return nil }
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return err};defer tx.Rollback()
 // Recheck after taking the write lock: another process may already have migrated.
 if _,err:=tx.ExecContext(ctx,`UPDATE meta SET value=value WHERE key='schema_version'`);err!=nil{return err}
 var current string
 if err:=tx.QueryRowContext(ctx,`SELECT value FROM meta WHERE key='schema_version'`).Scan(&current);err!=nil{return err}
 if current!="1"{return tx.Commit()}
 for _,statement:=range []string{
 `CREATE TABLE chunks_v2 (id INTEGER PRIMARY KEY AUTOINCREMENT, document_id INTEGER NOT NULL, ordinal INTEGER NOT NULL, page_start INTEGER, page_end INTEGER, heading TEXT, text TEXT NOT NULL, embedding_hash BLOB NOT NULL, embedding BLOB NOT NULL, UNIQUE(document_id,ordinal), FOREIGN KEY(document_id) REFERENCES documents(id) ON DELETE CASCADE)`,
 `INSERT INTO chunks_v2 SELECT * FROM chunks`,
 `DROP TABLE chunks`,
 `ALTER TABLE chunks_v2 RENAME TO chunks`,
 `CREATE INDEX chunks_hash ON chunks(document_id,embedding_hash)`,
 } { if _,err:=tx.ExecContext(ctx,statement);err!=nil{return fmt.Errorf("migrate chunk IDs: %w",err)} }
 if _,err:=tx.ExecContext(ctx,`INSERT INTO sqlite_sequence(name,seq) SELECT 'chunks',0 WHERE NOT EXISTS(SELECT 1 FROM sqlite_sequence WHERE name='chunks')`);err!=nil{return err}
 // Legacy IDs could have been deleted already. Reserve a new, JSON-safe range
 // so outstanding references issued by the v1 binary cannot alias new evidence.
 if _,err:=tx.ExecContext(ctx,`UPDATE sqlite_sequence SET seq=max(seq,?) WHERE name='chunks'`,time.Now().UnixMilli()*1000);err!=nil{return err}
 rows,err:=tx.QueryContext(ctx,`SELECT id,embedding FROM chunks ORDER BY id`);if err!=nil{return err}
 type vectorRow struct{id int64;raw []byte};var data []vectorRow
 for rows.Next(){var r vectorRow;if err:=rows.Scan(&r.id,&r.raw);err!=nil{rows.Close();return err};data=append(data,r)}
 err=rows.Err();rows.Close();if err!=nil{return err}
 for _,r:=range data{
  if len(r.raw)%4!=0{return fmt.Errorf("chunk %d has malformed vector",r.id)}
  v:=decodeVector(r.raw);if err:=validateVector(v);err!=nil{return err}
  if err:=ensureVectors(ctx,tx,len(v));err!=nil{return err}
  if _,err:=tx.ExecContext(ctx,`INSERT INTO chunks_vec(rowid,embedding) VALUES(?,?)`,r.id,r.raw);err!=nil{return err}
 }
 if _,err:=tx.ExecContext(ctx,`UPDATE meta SET value=? WHERE key='schema_version'`,SchemaVersion);err!=nil{return err}
 return tx.Commit()
}

type Candidate struct {
 Chunk Chunk
 VectorScore float64
 VectorRank int
 LexicalRank int
}

// Candidates reads vector ranks, lexical ranks and source text from one SQLite
// snapshot. Only the bounded union of candidates is copied into Go.
func (s *Store) Candidates(ctx context.Context,vector []float32,query,prefix string,limit int)([]Candidate,error){
 if err:=validateVector(vector);err!=nil{return nil,err}
 if limit<1||limit>100{return nil,fmt.Errorf("invalid candidate limit")}
 tx,err:=s.db.BeginTx(ctx,&sql.TxOptions{ReadOnly:true});if err!=nil{return nil,err};defer tx.Rollback()
 return candidatesInSnapshot(ctx,tx,vector,query,prefix,limit)
}

func candidatesInSnapshot(ctx context.Context,tx *sql.Tx,vector []float32,query,prefix string,limit int)([]Candidate,error){
 var dimensions string
 err:=tx.QueryRowContext(ctx,`SELECT value FROM meta WHERE key='vector_dimensions'`).Scan(&dimensions)
 if err!=nil&&err!=sql.ErrNoRows{return nil,err}
 candidates:=map[int64]*Candidate{}
 var norm float64;for _,v:=range vector{norm+=float64(v)*float64(v)}
 if err==nil {
  if dimensions!=strconv.Itoa(len(vector)){return nil,fmt.Errorf("query vector dimensions differ from index")}
  if norm>0 {
   q:=`SELECT rowid,distance FROM chunks_vec WHERE embedding MATCH ? AND k=?`
   args:=[]any{encodeVector(vector),limit}
   if prefix!="" {q+=` AND rowid IN (SELECT c.id FROM chunks c JOIN documents d ON d.id=c.document_id WHERE instr(d.relative_path,?)=1)`;args=append(args,normalizedPrefix(prefix))}
   q+=` ORDER BY distance`
   rows,err:=tx.QueryContext(ctx,q,args...);if err!=nil{return nil,err}
   rank:=0
   for rows.Next(){var id int64;var distance sql.NullFloat64;if err:=rows.Scan(&id,&distance);err!=nil{rows.Close();return nil,err};if !distance.Valid||math.IsNaN(distance.Float64)||distance.Float64>=1{continue};rank++;candidates[id]=&Candidate{VectorRank:rank,VectorScore:1-distance.Float64}}
   err=rows.Err();rows.Close();if err!=nil{return nil,err}
  }
 }
 ids,err:=searchFTS(ctx,tx,query,prefix,limit);if err!=nil{return nil,err}
 for rank,id:=range ids {if candidates[id]==nil{candidates[id]=&Candidate{}};candidates[id].LexicalRank=rank+1}
 if len(candidates)==0{return []Candidate{},nil}
 marks:=make([]string,0,len(candidates));args:=make([]any,0,len(candidates))
 for id:=range candidates{marks=append(marks,"?");args=append(args,id)}
 chunks,err:=queryChunksWith(ctx,tx,`WHERE c.id IN (`+strings.Join(marks,",")+`) ORDER BY c.id`,args...);if err!=nil{return nil,err}
 if len(chunks)!=len(candidates){return nil,fmt.Errorf("vector index contains missing chunks; run doctor")}
 out:=make([]Candidate,0,len(chunks));for _,chunk:=range chunks{c:=candidates[chunk.ID];c.Chunk=chunk;out=append(out,*c)}
 return out,nil
}

func (s *Store) vectorIntegrity(ctx context.Context)error{
 dimensions,err:=s.Meta(ctx,"vector_dimensions");if err!=nil{return err}
 if dimensions==""{var count int;if err:=s.db.QueryRowContext(ctx,`SELECT count(*) FROM chunks`).Scan(&count);err!=nil{return err};if count!=0{return fmt.Errorf("vector index missing")};return nil}
 for _,q:=range []string{
 `SELECT count(*) FROM chunks c LEFT JOIN chunks_vec v ON v.rowid=c.id WHERE v.rowid IS NULL`,
 `SELECT count(*) FROM chunks_vec v LEFT JOIN chunks c ON c.id=v.rowid WHERE c.id IS NULL`,
 `SELECT count(*) FROM chunks c JOIN chunks_vec v ON v.rowid=c.id WHERE c.embedding<>v.embedding`,
 }{var count int;if err:=s.db.QueryRowContext(ctx,q).Scan(&count);err!=nil{return err};if count>0{return fmt.Errorf("vector index inconsistent: %d rows",count)}}
 return nil
}
