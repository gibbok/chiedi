package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gibbok/local-genius/internal/embedding"
	"github.com/gibbok/local-genius/internal/indexer"
	"github.com/gibbok/local-genius/internal/mcp"
	"github.com/gibbok/local-genius/internal/retrieval"
	"github.com/gibbok/local-genius/internal/store"
)

const Version="0.1.0"

func Run(ctx context.Context,args []string,stdin io.Reader,stdout,stderr io.Writer)int{
	global:=flag.NewFlagSet("docdex",flag.ContinueOnError);global.SetOutput(stderr);defaultDB:=os.Getenv("DOCDEX_DB");if defaultDB==""{defaultDB="docdex.db"};dbPath:=global.String("db",defaultDB,"SQLite index path")
	if err:=global.Parse(args);err!=nil{return 2};rest:=global.Args();if len(rest)==0{usage(stderr);return 2};command:=rest[0]
	if command=="version"{fmt.Fprintln(stdout,"docdex "+Version);return 0}
	known:=map[string]bool{"init":true,"add":true,"remove":true,"roots":true,"index":true,"reconcile":true,"search":true,"status":true,"doctor":true,"mcp":true};if !known[command]{usage(stderr);fmt.Fprintf(stderr,"docdex: unknown command %q\n",command);return 2}
	s,err:=store.Open(ctx,*dbPath);if err!=nil{fmt.Fprintln(stderr,"docdex:",err);return 1};defer s.Close()
	embedder:=embedding.Projection{};idx:=indexer.Indexer{Store:s,Embedder:embedder};ret:=retrieval.Retriever{Store:s,Embedder:embedder}
	fail:=func(err error)int{fmt.Fprintln(stderr,"docdex:",err);return 1}
	switch command{
	case "init":fmt.Fprintln(stdout,s.Path());return 0
	case "add":if len(rest)!=2{return fail(errors.New("usage: docdex add <directory>"))};if err:=s.AddRoot(ctx,rest[1]);err!=nil{return fail(err)};fmt.Fprintln(stdout,"added",rest[1]);return 0
	case "remove":if len(rest)!=2{return fail(errors.New("usage: docdex remove <directory>"))};if err:=s.RemoveRoot(ctx,rest[1]);err!=nil{return fail(err)};fmt.Fprintln(stdout,"removed",rest[1]);return 0
	case "roots":roots,err:=s.Roots(ctx);if err!=nil{return fail(err)};return printJSON(stdout,roots,stderr)
	case "index","reconcile":stats,err:=idx.Reconcile(ctx);outputCode:=printJSON(stdout,stats,stderr);if err!=nil{return fail(err)};return outputCode
	case "status":status,err:=mcp.Status(ctx,s);if err!=nil{return fail(err)};return printJSON(stdout,status,stderr)
	case "doctor":if err:=s.IntegrityCheck(ctx);err!=nil{return fail(err)};if err:=idx.ValidateCompatibility(ctx);err!=nil{return fail(err)};if err:=s.IndexIntegrityCheck(ctx,embedder.Dimensions());err!=nil{return fail(err)};fmt.Fprintf(stdout,"ok: SQLite schema %d, model %s (%d dimensions), offline\n",store.SchemaVersion,embedder.ID(),embedder.Dimensions());return 0
	case "search":
		search:=flag.NewFlagSet("search",flag.ContinueOnError);search.SetOutput(stderr);limit:=search.Int("limit",10,"maximum results");prefix:=search.String("path-prefix","","relative path prefix");if err:=search.Parse(rest[1:]);err!=nil{return 2};question:=strings.Join(search.Args()," ");if question==""{return fail(errors.New("usage: docdex search [--limit N] <question>"))}
		if _,err:=idx.Reconcile(ctx);err!=nil{return fail(fmt.Errorf("reconcile index: %w",err))}
		results,err:=ret.Retrieve(ctx,question,*prefix,*limit);if err!=nil{return fail(err)};for _,r:=range results{location:=r.Path;if r.RootPath!=""{location=filepath.Join(r.RootPath,filepath.FromSlash(r.Path))};if r.Heading!=""{location+=" — "+r.Heading};if r.PageStart>0{location+=" — page "+strconv.Itoa(r.PageStart)};fmt.Fprintf(stdout,"[%d] %s\n%s\n\n",r.Rank,location,r.Text)};return 0
	case "mcp":server:=mcp.Server{Store:s,Indexer:idx,Retriever:ret};if err:=server.Serve(ctx,stdin,stdout);err!=nil&&!errors.Is(err,context.Canceled){return fail(err)};return 0
	}
	return 2
}

func printJSON(w io.Writer,v any,stderr io.Writer)int{enc:=json.NewEncoder(w);enc.SetIndent("","  ");if err:=enc.Encode(v);err!=nil{fmt.Fprintln(stderr,"docdex:",err);return 1};return 0}
func usage(w io.Writer){fmt.Fprintln(w,"usage: docdex [--db PATH] <init|add|remove|roots|index|reconcile|search|status|doctor|mcp|version>")}
