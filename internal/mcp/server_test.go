package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
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
