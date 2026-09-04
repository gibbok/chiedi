package indexer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/gibbok/local-genius/internal/chunk"
	"github.com/gibbok/local-genius/internal/embedding"
	"github.com/gibbok/local-genius/internal/extract"
	"github.com/gibbok/local-genius/internal/store"
)

type Stats struct {
	ScannedFiles   int      `json:"scanned_files"`
	ExtractionJobs int      `json:"extraction_jobs"`
	EmbeddingJobs  int      `json:"embedding_jobs"`
	ReusedChunks   int      `json:"reused_chunks"`
	Renamed        int      `json:"renamed_documents"`
	Deleted        int      `json:"deleted_documents"`
	Failed         int      `json:"failed_documents"`
	Errors         []string `json:"errors,omitempty"`
	CompletedAt    string   `json:"completed_at"`
}

type Indexer struct {
	Store    *store.Store
	Embedder embedding.Embedder
}

func (i Indexer) Reconcile(ctx context.Context) (Stats,error) {
	if i.Store==nil||i.Embedder==nil{return Stats{},fmt.Errorf("store and embedder are required")}
	roots,err:=i.Store.Roots(ctx);if err!=nil{return Stats{},err}
	stats:=Stats{}
	for _,root:=range roots{
		if err:=i.reconcileRoot(ctx,root,&stats);err!=nil{stats.Errors=append(stats.Errors,err.Error())}
	}
	stats.CompletedAt=time.Now().UTC().Format(time.RFC3339Nano)
	b,_:=json.Marshal(stats);_ = i.Store.SetMeta(ctx,"last_reconciliation",string(b))
	_ = i.Store.SetMeta(ctx,"embedding_model_id",i.Embedder.ID())
	_ = i.Store.SetMeta(ctx,"embedding_dimensions",fmt.Sprint(i.Embedder.Dimensions()))
	if len(stats.Errors)>0{return stats,fmt.Errorf("reconciliation completed with %d error(s)",len(stats.Errors))}
	return stats,nil
}

func (i Indexer) reconcileRoot(ctx context.Context,root store.Root,stats *Stats)error{
	rootInfo, err := os.Stat(root.Path)
	if err != nil { return fmt.Errorf("root %s unavailable: %w", root.Path, err) }
	if !rootInfo.IsDir() { return fmt.Errorf("root %s is not a directory", root.Path) }
	existing,err:=i.Store.DocumentsByRoot(ctx,root.ID);if err!=nil{return err}
	byPath:=map[string]store.Document{};byIdentity:=map[string]store.Document{};seen:=map[int64]bool{}
	for _,d:=range existing{byPath[d.RelativePath]=d;if d.Identity!=""{byIdentity[d.Identity]=d}}
	walkErr:=filepath.WalkDir(root.Path,func(path string,entry fs.DirEntry,walkErr error)error{
		if walkErr!=nil{stats.Errors=append(stats.Errors,fmt.Sprintf("%s: %v",path,walkErr));return nil}
		if err:=ctx.Err();err!=nil{return err}
		if entry.Type()&os.ModeSymlink!=0{if entry.IsDir(){return filepath.SkipDir};return nil}
		if entry.IsDir(){return nil}
		ext:=strings.ToLower(filepath.Ext(entry.Name()));if ext!=".txt"&&ext!=".md"&&ext!=".pdf"{return nil}
		stats.ScannedFiles++
		rel,err:=filepath.Rel(root.Path,path);if err!=nil{return err};rel=filepath.ToSlash(rel)
		info,err:=entry.Info();if err!=nil{stats.Errors=append(stats.Errors,fmt.Sprintf("%s: %v",rel,err));return nil}
		identity:=fileIdentity(info)
		old,found:=byPath[rel]
		if !found&&identity!=""{if candidate,ok:=byIdentity[identity];ok&&!seen[candidate.ID]{old=candidate;found=true
			if candidate.Size==info.Size()&&candidate.MtimeNS==info.ModTime().UnixNano(){
				if err:=i.Store.RenameDocument(ctx,candidate.ID,rel,info.Size(),info.ModTime().UnixNano());err!=nil{return err};seen[candidate.ID]=true;stats.Renamed++;return nil
			}
			if err:=i.Store.RenameDocument(ctx,candidate.ID,rel,info.Size(),info.ModTime().UnixNano());err!=nil{return err};old.RelativePath=rel;stats.Renamed++
		}}
		if found{seen[old.ID]=true;if old.Size==info.Size()&&old.MtimeNS==info.ModTime().UnixNano(){return nil}}
		content,err:=os.ReadFile(path);if err!=nil{stats.Errors=append(stats.Errors,fmt.Sprintf("%s: %v",rel,err));return nil}
		hash:=sha256.Sum256(content)
		if found&&bytes.Equal(old.ContentHash,hash[:]){
			if old.RelativePath!=rel{if err:=i.Store.RenameDocument(ctx,old.ID,rel,info.Size(),info.ModTime().UnixNano());err!=nil{return err};stats.Renamed++}else{if err:=i.Store.RenameDocument(ctx,old.ID,rel,info.Size(),info.ModTime().UnixNano());err!=nil{return err}}
			return nil
		}
		stats.ExtractionJobs++
		doc,extractErr:=extract.File(ctx,path)
		if extractErr!=nil{
			stats.Failed++
			_,err=i.Store.ReplaceDocument(ctx,store.Replacement{RootID:root.ID,RelativePath:rel,Identity:identity,Size:info.Size(),MtimeNS:info.ModTime().UnixNano(),ContentHash:hash[:],Format:strings.TrimPrefix(ext,"."),Status:"failed",Error:extractErr.Error()})
			if err!=nil{return err};if found{seen[old.ID]=true};return nil
		}
		newChunks:=chunk.Document(doc)
		reuse:=map[string][][]float32{}
		if found{
			oldChunks,err:=i.Store.ChunksForDocument(ctx,old.ID);if err!=nil{return err}
			for _,c:=range oldChunks{k:=string(c.EmbeddingHash);reuse[k]=append(reuse[k],c.Vector)}
		}
		stored:=make([]store.Chunk,len(newChunks));var pending []string;var pendingAt []int
		for n,c:=range newChunks{
			stored[n]=store.Chunk{Ordinal:c.Ordinal,Heading:c.Heading,PageStart:c.PageStart,PageEnd:c.PageEnd,Text:c.Text,EmbeddingHash:c.EmbeddingHash[:]}
			key:=string(c.EmbeddingHash[:]);if vectors:=reuse[key];len(vectors)>0{stored[n].Vector=vectors[0];reuse[key]=vectors[1:];stats.ReusedChunks++}else{pending=append(pending,c.EmbeddingText);pendingAt=append(pendingAt,n)}
		}
		if len(pending)>0{vectors,err:=i.Embedder.Embed(ctx,pending);if err!=nil{return err};for n,v:=range vectors{stored[pendingAt[n]].Vector=v};stats.EmbeddingJobs+=len(pending)}
		id,err:=i.Store.ReplaceDocument(ctx,store.Replacement{RootID:root.ID,RelativePath:rel,Identity:identity,Size:info.Size(),MtimeNS:info.ModTime().UnixNano(),ContentHash:hash[:],Format:strings.TrimPrefix(ext,"."),Status:"indexed",Chunks:stored});if err!=nil{return err};seen[id]=true
		return nil
	})
	if walkErr!=nil{return walkErr}
	for _,d:=range existing{if !seen[d.ID]{if err:=i.Store.DeleteDocument(ctx,d.ID);err!=nil{return err};stats.Deleted++}}
	return nil
}

func fileIdentity(info os.FileInfo)string{
	v:=reflect.ValueOf(info.Sys());if !v.IsValid(){return ""};if v.Kind()==reflect.Pointer{v=v.Elem()};if v.Kind()!=reflect.Struct{return ""}
	dev,ino:=v.FieldByName("Dev"),v.FieldByName("Ino");if !dev.IsValid()||!ino.IsValid(){return ""}
	return fmt.Sprintf("%v:%v",value(dev),value(ino))
}

func value(v reflect.Value)any{switch v.Kind(){case reflect.Uint,reflect.Uint8,reflect.Uint16,reflect.Uint32,reflect.Uint64:return v.Uint();case reflect.Int,reflect.Int8,reflect.Int16,reflect.Int32,reflect.Int64:return v.Int()};return ""}
