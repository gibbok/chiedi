package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gibbok/local-genius/internal/embedding"
	"github.com/gibbok/local-genius/internal/indexer"
	"github.com/gibbok/local-genius/internal/retrieval"
	"github.com/gibbok/local-genius/internal/store"
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
	if responses[2]["result"]==nil||responses[2]["error"]!=nil{t.Fatalf("initialize failed: %+v",responses[2])}
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
	ctx:=context.Background();s,err:=store.Open(ctx,filepath.Join(t.TempDir(),"empty.db"));if err!=nil{t.Fatal(err)};defer s.Close();embedder:=embedding.Projection{};server:=Server{Store:s,Indexer:indexer.Indexer{Store:s,Embedder:embedder},Retriever:retrieval.Retriever{Store:s,Embedder:embedder}};value,err:=server.call(ctx,"list_documents",json.RawMessage(`{}`));if err!=nil{t.Fatal(err)};encoded,err:=json.Marshal(value);if err!=nil{t.Fatal(err)};if string(encoded)!="[]"{t.Fatalf("empty documents encoded as %s",encoded)}
}
