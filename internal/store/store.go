package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const SchemaVersion = 1

type Store struct {
	db   *sql.DB
	path string
}

type Root struct {
	ID   int64  `json:"id"`
	Path string `json:"path"`
}

type Document struct {
	ID           int64  `json:"id"`
	RootID       int64  `json:"root_id"`
	RelativePath string `json:"path"`
	Identity     string `json:"file_identity,omitempty"`
	Size         int64  `json:"size_bytes"`
	MtimeNS      int64  `json:"mtime_ns"`
	ContentHash  []byte `json:"-"`
	Format       string `json:"format"`
	Status       string `json:"status"`
	Error        string `json:"error,omitempty"`
}

type Chunk struct {
	ID            int64     `json:"chunk_id"`
	DocumentID    int64     `json:"document_id"`
	Ordinal       int       `json:"ordinal"`
	Path          string    `json:"path"`
	Heading       string    `json:"heading,omitempty"`
	PageStart     int       `json:"page_start,omitempty"`
	PageEnd       int       `json:"page_end,omitempty"`
	Text          string    `json:"text"`
	EmbeddingHash []byte    `json:"-"`
	Vector        []float32 `json:"-"`
}

type Replacement struct {
	RootID       int64
	RelativePath string
	Identity     string
	Size         int64
	MtimeNS      int64
	ContentHash  []byte
	Format       string
	Status       string
	Error        string
	Chunks       []Chunk
}

type Counts struct {
	Roots     int `json:"configured_roots"`
	Documents int `json:"document_count"`
	Indexed   int `json:"indexed_count"`
	Failed    int `json:"failed_count"`
	Chunks    int `json:"chunk_count"`
}

func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("database path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", abs)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, path: abs}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Path() string { return s.path }

func (s *Store) migrate(ctx context.Context) error {
	statements := []string{
		`PRAGMA journal_mode=WAL`, `PRAGMA synchronous=NORMAL`, `PRAGMA foreign_keys=ON`, `PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS roots (id INTEGER PRIMARY KEY, path TEXT NOT NULL UNIQUE, created_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS documents (
			id INTEGER PRIMARY KEY, root_id INTEGER NOT NULL, relative_path TEXT NOT NULL,
			file_identity TEXT, size_bytes INTEGER NOT NULL, mtime_ns INTEGER NOT NULL,
			content_hash BLOB, format TEXT NOT NULL, indexed_at INTEGER, status TEXT NOT NULL, error TEXT,
			UNIQUE(root_id, relative_path), FOREIGN KEY(root_id) REFERENCES roots(id) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS chunks (
			id INTEGER PRIMARY KEY, document_id INTEGER NOT NULL, ordinal INTEGER NOT NULL,
			page_start INTEGER, page_end INTEGER, heading TEXT, text TEXT NOT NULL,
			embedding_hash BLOB NOT NULL, embedding BLOB NOT NULL,
			UNIQUE(document_id, ordinal), FOREIGN KEY(document_id) REFERENCES documents(id) ON DELETE CASCADE)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(text, heading, path)`,
		`CREATE INDEX IF NOT EXISTS documents_identity ON documents(root_id, file_identity)`,
		`CREATE INDEX IF NOT EXISTS chunks_hash ON chunks(document_id, embedding_hash)`,
	}
	// Establish metadata first so a newer database is rejected before this
	// binary makes any schema changes or overwrites its version marker.
	for _, statement := range statements[:5] {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("database migration: %w", err)
		}
	}
	version, err := s.Meta(ctx, "schema_version")
	if err != nil { return err }
	if version != "" {
		parsed, err := strconv.Atoi(version)
		if err != nil { return fmt.Errorf("invalid database schema version %q", version) }
		if parsed > SchemaVersion { return fmt.Errorf("database schema version %d is newer than supported version %d", parsed, SchemaVersion) }
	}
	for _, statement := range statements[5:] {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("database migration: %w", err)
		}
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES('schema_version',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, SchemaVersion)
	return err
}

func (s *Store) AddRoot(ctx context.Context, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return err
	}
	info, err := os.Stat(real)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("root is not a directory")
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO roots(path,created_at) VALUES(?,?) ON CONFLICT(path) DO NOTHING`, filepath.Clean(real), time.Now().Unix())
	return err
}

func (s *Store) RemoveRoot(ctx context.Context, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	canonical := filepath.Clean(abs)
	if real, evalErr := filepath.EvalSymlinks(abs); evalErr == nil { canonical = filepath.Clean(real) }
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT c.id FROM chunks c JOIN documents d ON d.id=c.document_id JOIN roots r ON r.id=d.root_id WHERE r.path=?`, canonical)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM chunks_fts WHERE rowid=?`, id); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM roots WHERE path=?`, canonical); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Roots(ctx context.Context) ([]Root, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,path FROM roots ORDER BY path`)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []Root
	for rows.Next() {
		var r Root
		if err := rows.Scan(&r.ID, &r.Path); err != nil { return nil, err }
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) DocumentsByRoot(ctx context.Context, rootID int64) ([]Document, error) {
	return s.documents(ctx, `WHERE root_id=?`, rootID)
}

func (s *Store) ListDocuments(ctx context.Context, prefix, extension, status string) ([]Document, error) {
	clauses := []string{"1=1"}
	args := []any{}
	if prefix != "" { clauses = append(clauses, "relative_path LIKE ?"); args = append(args, prefix+"%") }
	if extension != "" { clauses = append(clauses, "format=?"); args = append(args, strings.TrimPrefix(strings.ToLower(extension), ".")) }
	if status != "" { clauses = append(clauses, "status=?"); args = append(args, status) }
	return s.documents(ctx, "WHERE "+strings.Join(clauses, " AND "), args...)
}

func (s *Store) documents(ctx context.Context, where string, args ...any) ([]Document, error) {
	q := `SELECT id,root_id,relative_path,COALESCE(file_identity,''),size_bytes,mtime_ns,content_hash,format,status,COALESCE(error,'') FROM documents `+where+` ORDER BY relative_path`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []Document
	for rows.Next() {
		var d Document
		if err := rows.Scan(&d.ID,&d.RootID,&d.RelativePath,&d.Identity,&d.Size,&d.MtimeNS,&d.ContentHash,&d.Format,&d.Status,&d.Error); err != nil { return nil, err }
		out = append(out,d)
	}
	return out, rows.Err()
}

func (s *Store) ChunksForDocument(ctx context.Context, documentID int64) ([]Chunk, error) {
	return s.queryChunks(ctx, `WHERE c.document_id=? ORDER BY c.ordinal`, documentID)
}

func (s *Store) AllChunks(ctx context.Context, pathPrefix string) ([]Chunk, error) {
	where, args := "", []any{}
	if pathPrefix != "" { where = `WHERE d.relative_path LIKE ?`; args = append(args, pathPrefix+"%") }
	return s.queryChunks(ctx, where+` ORDER BY c.id`, args...)
}

func (s *Store) queryChunks(ctx context.Context, suffix string, args ...any) ([]Chunk, error) {
	q := `SELECT c.id,c.document_id,c.ordinal,d.relative_path,COALESCE(c.heading,''),COALESCE(c.page_start,0),COALESCE(c.page_end,0),c.text,c.embedding_hash,c.embedding FROM chunks c JOIN documents d ON d.id=c.document_id `+suffix
	rows, err := s.db.QueryContext(ctx,q,args...)
	if err != nil { return nil,err }
	defer rows.Close()
	var out []Chunk
	for rows.Next() {
		var c Chunk
		var vector []byte
		if err := rows.Scan(&c.ID,&c.DocumentID,&c.Ordinal,&c.Path,&c.Heading,&c.PageStart,&c.PageEnd,&c.Text,&c.EmbeddingHash,&vector); err != nil { return nil,err }
		c.Vector = decodeVector(vector)
		out = append(out,c)
	}
	return out,rows.Err()
}

func (s *Store) ReplaceDocument(ctx context.Context, replacement Replacement) (int64, error) {
	tx, err := s.db.BeginTx(ctx,nil)
	if err != nil { return 0,err }
	defer tx.Rollback()
	var documentID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM documents WHERE root_id=? AND relative_path=?`, replacement.RootID,replacement.RelativePath).Scan(&documentID)
	if err != nil && !errors.Is(err,sql.ErrNoRows) { return 0,err }
	if errors.Is(err,sql.ErrNoRows) {
		res, err := tx.ExecContext(ctx, `INSERT INTO documents(root_id,relative_path,file_identity,size_bytes,mtime_ns,content_hash,format,indexed_at,status,error) VALUES(?,?,?,?,?,?,?,?,?,?)`, replacement.RootID,replacement.RelativePath,replacement.Identity,replacement.Size,replacement.MtimeNS,replacement.ContentHash,replacement.Format,time.Now().UnixNano(),replacement.Status,replacement.Error)
		if err != nil { return 0,err }
		documentID,err=res.LastInsertId(); if err != nil { return 0,err }
	} else {
		rows,err:=tx.QueryContext(ctx,`SELECT id FROM chunks WHERE document_id=?`,documentID); if err!=nil{return 0,err}
		var ids []int64
		for rows.Next(){var id int64;if err:=rows.Scan(&id);err!=nil{rows.Close();return 0,err};ids=append(ids,id)}
		rows.Close()
		for _,id:=range ids{if _,err:=tx.ExecContext(ctx,`DELETE FROM chunks_fts WHERE rowid=?`,id);err!=nil{return 0,err}}
		if _,err:=tx.ExecContext(ctx,`DELETE FROM chunks WHERE document_id=?`,documentID);err!=nil{return 0,err}
		if _,err:=tx.ExecContext(ctx, `UPDATE documents SET file_identity=?,size_bytes=?,mtime_ns=?,content_hash=?,format=?,indexed_at=?,status=?,error=? WHERE id=?`, replacement.Identity,replacement.Size,replacement.MtimeNS,replacement.ContentHash,replacement.Format,time.Now().UnixNano(),replacement.Status,replacement.Error,documentID);err!=nil{return 0,err}
	}
	for _,c:=range replacement.Chunks{
		res,err:=tx.ExecContext(ctx,`INSERT INTO chunks(document_id,ordinal,page_start,page_end,heading,text,embedding_hash,embedding) VALUES(?,?,?,?,?,?,?,?)`,documentID,c.Ordinal,nullInt(c.PageStart),nullInt(c.PageEnd),c.Heading,c.Text,c.EmbeddingHash,encodeVector(c.Vector));if err!=nil{return 0,err}
		id,err:=res.LastInsertId();if err!=nil{return 0,err}
		if _,err:=tx.ExecContext(ctx,`INSERT INTO chunks_fts(rowid,text,heading,path) VALUES(?,?,?,?)`,id,c.Text,c.Heading,replacement.RelativePath);err!=nil{return 0,err}
	}
	if err:=tx.Commit();err!=nil{return 0,err}
	return documentID,nil
}

func nullInt(v int) any { if v==0{return nil};return v }

func (s *Store) RenameDocument(ctx context.Context, id int64, relativePath string, size, mtime int64) error {
	tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return err};defer tx.Rollback()
	if _,err:=tx.ExecContext(ctx,`UPDATE documents SET relative_path=?,size_bytes=?,mtime_ns=? WHERE id=?`,relativePath,size,mtime,id);err!=nil{return err}
	rows,err:=tx.QueryContext(ctx,`SELECT id,text,COALESCE(heading,'') FROM chunks WHERE document_id=?`,id);if err!=nil{return err}
	type row struct{id int64;text,heading string};var data []row
	for rows.Next(){var r row;if err:=rows.Scan(&r.id,&r.text,&r.heading);err!=nil{rows.Close();return err};data=append(data,r)};rows.Close()
	for _,r:=range data{if _,err:=tx.ExecContext(ctx,`DELETE FROM chunks_fts WHERE rowid=?`,r.id);err!=nil{return err};if _,err:=tx.ExecContext(ctx,`INSERT INTO chunks_fts(rowid,text,heading,path) VALUES(?,?,?,?)`,r.id,r.text,r.heading,relativePath);err!=nil{return err}}
	return tx.Commit()
}

func (s *Store) DeleteDocument(ctx context.Context,id int64) error {
	tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return err};defer tx.Rollback()
	rows,err:=tx.QueryContext(ctx,`SELECT id FROM chunks WHERE document_id=?`,id);if err!=nil{return err};var ids []int64
	for rows.Next(){var n int64;if err:=rows.Scan(&n);err!=nil{rows.Close();return err};ids=append(ids,n)};rows.Close()
	for _,n:=range ids{if _,err:=tx.ExecContext(ctx,`DELETE FROM chunks_fts WHERE rowid=?`,n);err!=nil{return err}}
	if _,err:=tx.ExecContext(ctx,`DELETE FROM documents WHERE id=?`,id);err!=nil{return err}
	return tx.Commit()
}

func (s *Store) SearchFTS(ctx context.Context, query, pathPrefix string, limit int) ([]int64,error) {
	if strings.TrimSpace(query)=="" { return nil,nil }
	q:=`SELECT f.rowid FROM chunks_fts f JOIN chunks c ON c.id=f.rowid JOIN documents d ON d.id=c.document_id WHERE chunks_fts MATCH ?`
	args:=[]any{query}
	if pathPrefix!=""{q+=` AND d.relative_path LIKE ?`;args=append(args,pathPrefix+"%")}
	q+=` ORDER BY bm25(chunks_fts) LIMIT ?`;args=append(args,limit)
	rows,err:=s.db.QueryContext(ctx,q,args...);if err!=nil{return nil,err};defer rows.Close();var ids []int64
	for rows.Next(){var id int64;if err:=rows.Scan(&id);err!=nil{return nil,err};ids=append(ids,id)}
	return ids,rows.Err()
}

func (s *Store) ReadChunks(ctx context.Context, ids []int64, before, after int) ([]Chunk,error) {
	seen:=map[int64]bool{};var out []Chunk
	for _,id:=range ids{
		var docID int64;var ordinal int
		if err:=s.db.QueryRowContext(ctx,`SELECT document_id,ordinal FROM chunks WHERE id=?`,id).Scan(&docID,&ordinal);err!=nil{if errors.Is(err,sql.ErrNoRows){continue};return nil,err}
		chunks,err:=s.queryChunks(ctx,`WHERE c.document_id=? AND c.ordinal BETWEEN ? AND ? ORDER BY c.ordinal`,docID,ordinal-before,ordinal+after);if err!=nil{return nil,err}
		for _,c:=range chunks{if !seen[c.ID]{seen[c.ID]=true;out=append(out,c)}}
	}
	return out,nil
}

func (s *Store) Counts(ctx context.Context)(Counts,error){
	var c Counts
	queries:=[]struct{q string;dst *int}{{`SELECT count(*) FROM roots`,&c.Roots},{`SELECT count(*) FROM documents`,&c.Documents},{`SELECT count(*) FROM documents WHERE status='indexed'`,&c.Indexed},{`SELECT count(*) FROM documents WHERE status='failed'`,&c.Failed},{`SELECT count(*) FROM chunks`,&c.Chunks}}
	for _,item:=range queries{if err:=s.db.QueryRowContext(ctx,item.q).Scan(item.dst);err!=nil{return c,err}}
	return c,nil
}

func (s *Store) SetMeta(ctx context.Context,key,value string)error{_,err:=s.db.ExecContext(ctx,`INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,key,value);return err}
func (s *Store) Meta(ctx context.Context,key string)(string,error){var v string;err:=s.db.QueryRowContext(ctx,`SELECT value FROM meta WHERE key=?`,key).Scan(&v);if errors.Is(err,sql.ErrNoRows){return "",nil};return v,err}
func (s *Store) IntegrityCheck(ctx context.Context) error { var result string; if err:=s.db.QueryRowContext(ctx,`PRAGMA integrity_check`).Scan(&result);err!=nil{return err};if result!="ok"{return fmt.Errorf("SQLite integrity check: %s",result)};return nil }

func encodeVector(v []float32)[]byte{b:=make([]byte,len(v)*4);for i,x:=range v{binary.LittleEndian.PutUint32(b[i*4:],math.Float32bits(x))};return b}
func decodeVector(b []byte)[]float32{v:=make([]float32,len(b)/4);for i:=range v{v[i]=math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))};return v}
