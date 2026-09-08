package retrieval

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/gibbok/chiedi/internal/embedding"
	"github.com/gibbok/chiedi/internal/store"
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
	query:=queryTerms(question)
	qv,err:=r.Embedder.Embed(ctx,[]string{query});if err!=nil{return nil,err}
	if len(qv)!=1{return nil,fmt.Errorf("embedding model returned %d vectors for the query; expected 1",len(qv))}
	if len(qv[0])!=r.Embedder.Dimensions(){return nil,fmt.Errorf("embedding model returned %d query dimensions; expected %d",len(qv[0]),r.Embedder.Dimensions())}
	for _,x:=range qv[0]{if math.IsNaN(float64(x))||math.IsInf(float64(x),0){return nil,fmt.Errorf("embedding model returned a non-finite query vector value")}}
	candidates,err:=r.Store.Candidates(ctx,qv[0],ftsQuery(query),pathPrefix,40);if err!=nil{return nil,err}
	type combined struct{chunk store.Chunk;score,vector float64;lexRank int};byID:=map[int64]*combined{}
	for _,candidate:=range candidates {
	 c:=&combined{chunk:candidate.Chunk,vector:candidate.VectorScore,lexRank:candidate.LexicalRank}
	 if candidate.VectorRank>0{c.score+=1/float64(60+candidate.VectorRank)}
	 if candidate.LexicalRank>0{c.score+=1/float64(60+candidate.LexicalRank)}
	 byID[c.chunk.ID]=c
	}
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

// Question scaffolding should not outrank the subject of the question. Apply
// this only to queries, leaving stored embeddings and their identity unchanged.
func queryTerms(question string)string{
 stop:=map[string]bool{"a":true,"an":true,"the":true,"is":true,"are":true,"was":true,"were":true,"be":true,"been":true,"being":true,"do":true,"does":true,"did":true,"what":true,"which":true,"who":true,"when":true,"where":true,"why":true,"how":true,"can":true,"could":true,"would":true,"should":true,"will":true,"i":true,"we":true,"you":true,"it":true,"they":true,"he":true,"she":true,"of":true,"to":true,"in":true,"on":true,"at":true,"for":true,"from":true,"with":true,"and":true,"or":true,"here":true}
 words:=strings.FieldsFunc(strings.ToLower(question),func(r rune)bool{return !unicode.IsLetter(r)&&!unicode.IsNumber(r)})
 terms:=make([]string,0,len(words));for _,word:=range words{if !stop[word]{terms=append(terms,word)}}
 if len(terms)==0{return question};return strings.Join(terms," ")
}
