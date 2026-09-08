//go:build unix

package indexer

import (
 "context"
 "path/filepath"
 "syscall"
 "testing"
 "time"

 "github.com/gibbok/chiedi/internal/embedding"
 "github.com/gibbok/chiedi/internal/extract"
 "github.com/gibbok/chiedi/internal/store"
 "github.com/gibbok/chiedi/internal/source"
)

func TestNamedPipeIsSkippedWithoutBlocking(t *testing.T){
 root:=t.TempDir();path:=filepath.Join(root,"pipe.txt");if err:=syscall.Mkfifo(path,0600);err!=nil{t.Fatal(err)}
 ctx:=context.Background();s,err:=store.Open(ctx,filepath.Join(t.TempDir(),"fifo.db"));if err!=nil{t.Fatal(err)};defer s.Close();if err:=s.AddRoot(ctx,root);err!=nil{t.Fatal(err)}
 done:=make(chan error,1);go func(){_,err:=(Indexer{Store:s,Embedder:embedding.Projection{}}).Reconcile(ctx);done<-err}()
 select{case err:=<-done:if err!=nil{t.Fatal(err)};case <-time.After(time.Second):t.Fatal("indexer blocked on FIFO")}
 if _,err:=extract.File(ctx,path);err==nil{t.Fatal("extract accepted FIFO")}
 if f,err:=source.Open(path);err==nil{f.Close();t.Fatal("source open accepted FIFO")}
}
