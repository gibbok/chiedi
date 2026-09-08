package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/gibbok/local-genius/internal/indexer"
	"github.com/gibbok/local-genius/internal/retrieval"
	"github.com/gibbok/local-genius/internal/store"
)

type Server struct {
	Store     *store.Store
	Indexer   indexer.Indexer
	Retriever retrieval.Retriever
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      any         `json:"id"`
	Result  any         `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct{Code int `json:"code"`;Message string `json:"message"`}

func (s Server) Serve(ctx context.Context,in io.Reader,out io.Writer)error{
	type scanItem struct{line []byte;err error;done bool};items:=make(chan scanItem);scanner:=bufio.NewScanner(in);scanner.Buffer(make([]byte,64<<10),4<<20)
	go func(){for scanner.Scan(){item:=scanItem{line:append([]byte(nil),scanner.Bytes()...)};select{case items<-item:case <-ctx.Done():return}};select{case items<-scanItem{err:scanner.Err(),done:true}:case <-ctx.Done():}}()
	encoder:=json.NewEncoder(out)
	for {
		var item scanItem
		select{case <-ctx.Done():return ctx.Err();case item=<-items:}
		if item.done{return item.err}
		line:=item.line;if len(strings.TrimSpace(string(line)))==0{continue}
		var req request
		if err:=json.Unmarshal(line,&req);err!=nil{if err:=encoder.Encode(response{JSONRPC:"2.0",ID:nil,Error:&rpcError{-32700,"parse error"}});err!=nil{return err};continue}
		if len(req.ID)==0 { continue }
		id,validID:=responseID(req.ID);if !validID{if err:=encoder.Encode(response{JSONRPC:"2.0",ID:nil,Error:&rpcError{-32600,"invalid request"}});err!=nil{return err};continue}
		if req.JSONRPC!="2.0"||req.Method==""{if err:=encoder.Encode(response{JSONRPC:"2.0",ID:id,Error:&rpcError{-32600,"invalid request"}});err!=nil{return err};continue}
		result,rpcErr:=s.handle(ctx,req)
		if err:=encoder.Encode(response{JSONRPC:"2.0",ID:id,Result:result,Error:rpcErr});err!=nil{return err}
	}
}

func responseID(raw json.RawMessage)(any,bool){dec:=json.NewDecoder(bytes.NewReader(raw));dec.UseNumber();var value any;if err:=dec.Decode(&value);err!=nil{return nil,false};switch value.(type){case nil,string,json.Number:return json.RawMessage(append([]byte(nil),raw...)),true;default:return nil,false}}

func (s Server) handle(ctx context.Context,req request)(any,*rpcError){
	switch req.Method{
	case "initialize":
		return map[string]any{"protocolVersion":"2025-06-18","capabilities":map[string]any{"tools":map[string]any{"listChanged":false}},"serverInfo":map[string]any{"name":"docdex","version":"0.1.0"}},nil
	case "ping":return map[string]any{},nil
	case "tools/list":return map[string]any{"tools":toolDefinitions()},nil
	case "tools/call":
		var call struct{Name string `json:"name"`;Arguments json.RawMessage `json:"arguments"`};if err:=decodeArgs(req.Params,&call);err!=nil||call.Name==""{return nil,&rpcError{-32602,"invalid tool parameters"}}
		value,err:=s.call(ctx,call.Name,call.Arguments);if err!=nil{return map[string]any{"content":[]any{map[string]any{"type":"text","text":err.Error()}},"isError":true},nil}
		structured,ok:=value.(map[string]any);if !ok { structured=map[string]any{"results":value} };encoded,_:=json.Marshal(structured);return map[string]any{"content":[]any{map[string]any{"type":"text","text":string(encoded)}},"structuredContent":structured,"isError":false},nil
	default:return nil,&rpcError{-32601,"method not found"}
	}
}

func (s Server) call(ctx context.Context,name string,args json.RawMessage)(any,error){
	reconcile:=func()error{if _,err:=s.Indexer.Reconcile(ctx);err!=nil{return fmt.Errorf("reconcile index: %w",err)};return nil}
	switch name{
	case "retrieve":
		var a struct{Question string `json:"question"`;Limit int `json:"limit"`;PathPrefix string `json:"path_prefix"`};if err:=decodeArgs(args,&a);err!=nil{return nil,err};if strings.TrimSpace(a.Question)==""{return nil,fmt.Errorf("question is required")};if a.Limit<0||a.Limit>50{return nil,fmt.Errorf("limit must be between 1 and 50, or 0 for the default")};if err:=reconcile();err!=nil{return nil,err};return s.Retriever.Retrieve(ctx,a.Question,a.PathPrefix,a.Limit)
	case "read_chunks":
		var a struct{ChunkIDs []int64 `json:"chunk_ids"`;Before int `json:"before"`;After int `json:"after"`};if err:=decodeArgs(args,&a);err!=nil{return nil,err};if len(a.ChunkIDs)==0{return nil,fmt.Errorf("chunk_ids is required")};if len(a.ChunkIDs)>100{return nil,fmt.Errorf("chunk_ids may contain at most 100 IDs")};for _,id:=range a.ChunkIDs{if id<=0{return nil,fmt.Errorf("chunk_ids must contain only positive IDs")}};if a.Before<0||a.After<0||a.Before>5||a.After>5{return nil,fmt.Errorf("before/after must be between 0 and 5")};if err:=reconcile();err!=nil{return nil,err};return s.Store.ReadChunks(ctx,a.ChunkIDs,a.Before,a.After)
	case "list_documents":
		var a struct{PathPrefix string `json:"path_prefix"`;Extension string `json:"extension"`;Status string `json:"status"`};if err:=decodeArgs(args,&a);err!=nil{return nil,err};if a.Status!=""&&a.Status!="indexed"&&a.Status!="failed"{return nil,fmt.Errorf("status must be indexed or failed")};if err:=reconcile();err!=nil{return nil,err};return s.Store.ListDocuments(ctx,a.PathPrefix,a.Extension,a.Status)
	case "index_status":var a struct{};if err:=decodeArgs(args,&a);err!=nil{return nil,err};if err:=reconcile();err!=nil{return nil,err};return Status(ctx,s.Store)
	default:return nil,fmt.Errorf("unknown tool %q",name)
	}
}

func decodeArgs(raw json.RawMessage,dst any)error{trimmed:=bytes.TrimSpace(raw);if len(trimmed)==0{return nil};if bytes.Equal(trimmed,[]byte("null")){return fmt.Errorf("arguments must be an object")};dec:=json.NewDecoder(bytes.NewReader(trimmed));dec.DisallowUnknownFields();if err:=dec.Decode(dst);err!=nil{return err};var trailing any;if err:=dec.Decode(&trailing);err!=io.EOF{return fmt.Errorf("arguments must contain exactly one JSON object")};return nil}

func Status(ctx context.Context,s *store.Store)(map[string]any,error){
	counts,paths,err:=s.CountsWithFailedPaths(ctx);if err!=nil{return nil,err};last,err:=s.Meta(ctx,"last_reconciliation");if err!=nil{return nil,err};model,err:=s.Meta(ctx,"embedding_model_id");if err!=nil{return nil,err};dimensionsText,err:=s.Meta(ctx,"embedding_dimensions");if err!=nil{return nil,err};dimensions:=0;if dimensionsText!=""{dimensions,err=strconv.Atoi(dimensionsText);if err!=nil{return nil,fmt.Errorf("invalid stored embedding dimensions %q",dimensionsText)}}
	result:=map[string]any{"db_path":s.Path(),"schema_version":store.SchemaVersion,"embedding_model":model,"embedding_dimensions":dimensions,"counts":counts,"failed_document_paths":paths}
	if last!=""{var parsed any;if err:=json.Unmarshal([]byte(last),&parsed);err!=nil{return nil,fmt.Errorf("invalid stored reconciliation status: %w",err)};result["last_reconciliation"]=parsed}
	return result,nil
}

func toolDefinitions()[]map[string]any{return []map[string]any{
	{"name":"retrieve","description":"Retrieve semantically and lexically relevant document evidence.","inputSchema":map[string]any{"type":"object","additionalProperties":false,"required":[]string{"question"},"properties":map[string]any{"question":map[string]any{"type":"string","minLength":1},"limit":map[string]any{"type":"integer","minimum":1,"maximum":50},"path_prefix":map[string]any{"type":"string"}}}},
	{"name":"read_chunks","description":"Read chunks and bounded neighboring context.","inputSchema":map[string]any{"type":"object","additionalProperties":false,"required":[]string{"chunk_ids"},"properties":map[string]any{"chunk_ids":map[string]any{"type":"array","minItems":1,"maxItems":100,"items":map[string]any{"type":"integer","minimum":1}},"before":map[string]any{"type":"integer","minimum":0,"maximum":5},"after":map[string]any{"type":"integer","minimum":0,"maximum":5}}}},
	{"name":"list_documents","description":"List indexed documents.","inputSchema":map[string]any{"type":"object","additionalProperties":false,"properties":map[string]any{"path_prefix":map[string]any{"type":"string"},"extension":map[string]any{"type":"string"},"status":map[string]any{"type":"string","enum":[]string{"indexed","failed"}}}}},
	{"name":"index_status","description":"Inspect index state and last reconciliation work.","inputSchema":map[string]any{"type":"object","additionalProperties":false,"properties":map[string]any{}}},
}}
