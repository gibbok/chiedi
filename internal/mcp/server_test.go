package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
 "os"
 "reflect"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gibbok/chiedi/internal/embedding"
	"github.com/gibbok/chiedi/internal/indexer"
	"github.com/gibbok/chiedi/internal/retrieval"
	"github.com/gibbok/chiedi/internal/store"
)

func TestProtocolValidationAndInitialize(t *testing.T){
	input:=strings.Join([]string{
		`not-json`,
		`{"jsonrpc":"1.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{}}`,
	},"\n")
	var output bytes.Buffer
	if err:=(Server{}).Serve(context.Background(),strings.NewReader(input),&output);err!=nil{t.Fatal(err)}
	lines:=strings.Split(strings.TrimSpace(output.String()),"\n");if len(lines)!=3{t.Fatalf("responses=%d: %s",len(lines),output.String())}
	var responses []map[string]any
	for _,line:=range lines{var response map[string]any;if err:=json.Unmarshal([]byte(line),&response);err!=nil{t.Fatal(err)};responses=append(responses,response)}
	if responses[0]["error"]==nil||responses[1]["error"]==nil{t.Fatalf("invalid requests accepted: %+v",responses)}
	if id,exists:=responses[0]["id"];!exists||id!=nil{t.Fatalf("parse error must include id:null: %+v",responses[0])}
	if responses[2]["result"]==nil||responses[2]["error"]!=nil{t.Fatalf("initialize failed: %+v",responses[2])}
}

func TestRequestIDValidationAndPrecision(t *testing.T){
	input:=strings.Join([]string{`{"jsonrpc":"2.0","id":900719925474099312345,"method":"ping"}`,`{"jsonrpc":"2.0","id":{"bad":true},"method":"ping"}`},"\n");var output bytes.Buffer;if err:=(Server{}).Serve(context.Background(),strings.NewReader(input),&output);err!=nil{t.Fatal(err)};lines:=strings.Split(strings.TrimSpace(output.String()),"\n");if len(lines)!=2{t.Fatalf("responses=%d: %s",len(lines),output.String())};if !strings.Contains(lines[0],`"id":900719925474099312345`){t.Fatalf("large request ID lost precision: %s",lines[0])};var invalid map[string]any;if err:=json.Unmarshal([]byte(lines[1]),&invalid);err!=nil{t.Fatal(err)};if invalid["id"]!=nil||invalid["error"]==nil{t.Fatalf("invalid ID was accepted: %+v",invalid)}
}

func TestCancellationDoesNotWaitForStdin(t *testing.T){
	reader,writer:=io.Pipe();ctx,cancel:=context.WithCancel(context.Background());done:=make(chan error,1);go func(){done<-(Server{}).Serve(ctx,reader,io.Discard)}();cancel()
	select{case err:=<-done:if err!=context.Canceled{t.Fatalf("got %v",err)};case <-time.After(time.Second):t.Fatal("server did not stop after cancellation")};reader.Close();writer.Close()
}

func TestDecodeArgsRejectsAmbiguousJSON(t *testing.T){
	for _,raw:=range []string{`null`,`{} {}`,`{"unknown":true}`}{var dst struct{Known bool `json:"known"`};if err:=decodeArgs(json.RawMessage(raw),&dst);err==nil{t.Fatalf("accepted %s",raw)}}
}

func TestInvalidToolCallsAreRejectedBeforeReconciliation(t *testing.T){
	ids:=make([]int64,101);for n:=range ids{ids[n]=int64(n+1)};tooMany,_:=json.Marshal(map[string]any{"chunk_ids":ids})
	tests:=[]struct{name string;args json.RawMessage;want string}{{"unknown",json.RawMessage(`{}`),"unknown tool"},{"retrieve",json.RawMessage(`{"question":" ","limit":1}`),"question is required"},{"read_chunks",tooMany,"at most 100"},{"read_chunks",json.RawMessage(`{"chunk_ids":[0]}`),"positive IDs"},{"list_documents",json.RawMessage(`{"status":"mystery"}`),"status must be"},{"index_status",json.RawMessage(`{"unknown":true}`),"unknown field"}}
	for _,test:=range tests{t.Run(test.name+test.want,func(t *testing.T){_,err:=(Server{}).call(context.Background(),test.name,test.args);if err==nil||!strings.Contains(err.Error(),test.want){t.Fatalf("got %v, want %q",err,test.want)}})}
}

func TestStatusRejectsCorruptReconciliationMetadata(t *testing.T){
	ctx:=context.Background();s,err:=store.Open(ctx,filepath.Join(t.TempDir(),"status.db"));if err!=nil{t.Fatal(err)};defer s.Close();if err:=s.SetMeta(ctx,"last_reconciliation","{");err!=nil{t.Fatal(err)};if _,err:=Status(ctx,s);err==nil||!strings.Contains(err.Error(),"invalid stored reconciliation status"){t.Fatalf("got %v",err)}
}

func TestListDocumentsReturnsAnEmptyArray(t *testing.T){
	ctx:=context.Background();s,err:=store.Open(ctx,filepath.Join(t.TempDir(),"empty.db"));if err!=nil{t.Fatal(err)};defer s.Close();embedder:=embedding.E5{};server:=Server{Store:s,Indexer:indexer.Indexer{Store:s,Embedder:embedder},Retriever:retrieval.Retriever{Store:s,Embedder:embedder}};value,err:=server.call(ctx,"list_documents",json.RawMessage(`{}`));if err!=nil{t.Fatal(err)};encoded,err:=json.Marshal(value);if err!=nil{t.Fatal(err)};if string(encoded)!="[]"{t.Fatalf("empty documents encoded as %s",encoded)}
}

func TestToolResultsConformToMCPObjectContract(t *testing.T){
 ctx:=context.Background();s,err:=store.Open(ctx,filepath.Join(t.TempDir(),"contract.db"));if err!=nil{t.Fatal(err)};defer s.Close()
 embedder:=embedding.E5{};server:=Server{Store:s,Indexer:indexer.Indexer{Store:s,Embedder:embedder},Retriever:retrieval.Retriever{Store:s,Embedder:embedder}}
 root:=t.TempDir();path:=filepath.Join(root,"evidence.txt");if err:=os.WriteFile(path,[]byte("known document evidence"),0600);err!=nil{t.Fatal(err)};if err:=s.AddRoot(ctx,root);err!=nil{t.Fatal(err)}
 if _,err:=server.Indexer.Reconcile(ctx);err!=nil{t.Fatal(err)};chunks,err:=s.AllChunks(ctx,"");if err!=nil||len(chunks)!=1{t.Fatalf("chunks: %+v %v",chunks,err)}
 for _,call:=range []struct{name string;args any}{{"retrieve",map[string]any{"question":"known evidence"}},{"read_chunks",map[string]any{"chunk_ids":[]int64{chunks[0].ID}}},{"list_documents",map[string]any{}},{"index_status",map[string]any{}}}{
  params,_:=json.Marshal(map[string]any{"name":call.name,"arguments":call.args});value,rpcErr:=server.handle(ctx,request{Method:"tools/call",Params:params});if rpcErr!=nil{t.Fatal(rpcErr)}
  raw,_:=json.Marshal(value);var result struct{Content []struct{Type string `json:"type"`;Text string `json:"text"`} `json:"content"`;Structured map[string]json.RawMessage `json:"structuredContent"`;IsError bool `json:"isError"`}
  if err:=json.Unmarshal(raw,&result);err!=nil{t.Fatalf("%s response violates MCP object contract: %s: %v",call.name,raw,err)}
  if result.IsError||result.Structured==nil||len(result.Content)!=1{t.Fatalf("%s failed: %s",call.name,raw)}
  var textValue map[string]json.RawMessage;if err:=json.Unmarshal([]byte(result.Content[0].Text),&textValue);err!=nil{t.Fatal(err)}
  if !reflect.DeepEqual(result.Structured,textValue){t.Fatal("text and structured content disagree")}
 }
 if err:=os.WriteFile(path,[]byte("different replacement evidence with changed size"),0600);err!=nil{t.Fatal(err)}
 args,_:=json.Marshal(map[string]any{"chunk_ids":[]int64{chunks[0].ID}})
 if _,err:=server.call(ctx,"read_chunks",args);err==nil||!strings.Contains(err.Error(),"stale"){t.Fatalf("read_chunks silently accepted stale evidence: %v",err)}
}
