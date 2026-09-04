package retrieval

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/gibbok/local-genius/internal/embedding"
	"github.com/gibbok/local-genius/internal/store"
)

type Result struct {
	ChunkID      int64   `json:"chunk_id"`
	DocumentID   int64   `json:"document_id"`
	RootID       int64   `json:"root_id"`
	RootPath     string  `json:"root_path"`
	Path         string  `json:"path"`
	Heading      string  `json:"heading,omitempty"`
	PageStart    int     `json:"page_start,omitempty"`
	PageEnd      int     `json:"page_end,omitempty"`
	Text         string  `json:"text"`
	Rank         int     `json:"rank"`
	Score        float64 `json:"score"`
	VectorScore  float64 `json:"vector_score,omitempty"`
	LexicalRank  int     `json:"lexical_rank,omitempty"`
}

type Retriever struct{Store *store.Store;Embedder embedding.Embedder}

func (r Retriever) Retrieve(ctx context.Context,question,pathPrefix string,limit int)([]Result,error){
	if r.Store==nil||r.Embedder==nil{return nil,fmt.Errorf("store and embedder are required")}
	if strings.TrimSpace(question)==""{return nil,fmt.Errorf("question is required")};if limit<0||limit>50{return nil,fmt.Errorf("limit must be between 1 and 50, or 0 for the default")};if limit==0{limit=10}
	chunks,err:=r.Store.AllChunks(ctx,pathPrefix);if err!=nil{return nil,err}
	qv,err:=r.Embedder.Embed(ctx,[]string{question});if err!=nil{return nil,err}
	if len(qv)!=1{return nil,fmt.Errorf("embedding model returned %d vectors for the query; expected 1",len(qv))}
	if len(qv[0])!=r.Embedder.Dimensions(){return nil,fmt.Errorf("embedding model returned %d query dimensions; expected %d",len(qv[0]),r.Embedder.Dimensions())}
	for _,x:=range qv[0]{if math.IsNaN(float64(x))||math.IsInf(float64(x),0){return nil,fmt.Errorf("embedding model returned a non-finite query vector value")}}
	type scored struct{chunk store.Chunk;similarity float64};vectors:=make([]scored,0,len(chunks))
	for _,c:=range chunks{s,err:=embedding.Cosine(qv[0],c.Vector);if err!=nil{return nil,fmt.Errorf("chunk %d vector: %w",c.ID,err)};if s<=0{continue};vectors=append(vectors,scored{c,s})}
	sort.SliceStable(vectors,func(i,j int)bool{return vectors[i].similarity>vectors[j].similarity});if len(vectors)>40{vectors=vectors[:40]}
	lexical,err:=r.Store.SearchFTS(ctx,ftsQuery(question),pathPrefix,40);if err!=nil{return nil,err}
	type combined struct{chunk store.Chunk;score,vector float64;lexRank int};byID:=map[int64]*combined{}
	for rank,item:=range vectors{c:=&combined{chunk:item.chunk,vector:item.similarity};c.score+=1/float64(60+rank+1);byID[item.chunk.ID]=c}
	chunkByID:=map[int64]store.Chunk{};for _,c:=range chunks{chunkByID[c.ID]=c}
	for rank,id:=range lexical{c:=byID[id];if c==nil{c=&combined{chunk:chunkByID[id]};byID[id]=c};c.lexRank=rank+1;c.score+=1/float64(60+rank+1)}
	all:=make([]*combined,0,len(byID));for _,c:=range byID{all=append(all,c)};sort.SliceStable(all,func(i,j int)bool{if all[i].score!=all[j].score{return all[i].score>all[j].score};if all[i].vector!=all[j].vector{return all[i].vector>all[j].vector};return all[i].chunk.ID<all[j].chunk.ID});if len(all)>limit{all=all[:limit]}
	out:=make([]Result,len(all));for n,c:=range all{out[n]=Result{ChunkID:c.chunk.ID,DocumentID:c.chunk.DocumentID,RootID:c.chunk.RootID,RootPath:c.chunk.RootPath,Path:c.chunk.Path,Heading:c.chunk.Heading,PageStart:c.chunk.PageStart,PageEnd:c.chunk.PageEnd,Text:c.chunk.Text,Rank:n+1,Score:c.score,VectorScore:c.vector,LexicalRank:c.lexRank}}
	return out,nil
}

func ftsQuery(s string)string{
	words:=strings.FieldsFunc(strings.ToLower(s),func(r rune)bool{return !unicode.IsLetter(r)&&!unicode.IsNumber(r)})
	seen:=map[string]bool{};var terms []string
	for _,word:=range words{if len([]rune(word))<2||seen[word]{continue};seen[word]=true;terms=append(terms,`"`+strings.ReplaceAll(word,`"`,`""`)+`"`)}
	if len(terms)==0{return `"__no_match__"`};return strings.Join(terms," OR ")
}
