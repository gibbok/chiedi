//go:build !windows

package indexer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gibbok/chiedi/internal/embedding"
	"github.com/gibbok/chiedi/internal/store"
)

func TestTraversalErrorDefersAllStaleDeletion(t *testing.T){
	if os.Geteuid()==0{t.Skip("permission denial cannot be tested as root")}
	ctx:=context.Background();root:=t.TempDir();restricted:=filepath.Join(root,"restricted");if err:=os.Mkdir(restricted,0o700);err!=nil{t.Fatal(err)};visible:=filepath.Join(root,"visible.txt");writeAt(t,visible,"visible evidence",time.Now());writeAt(t,filepath.Join(restricted,"hidden.txt"),"hidden evidence",time.Now())
	s,err:=store.Open(ctx,filepath.Join(t.TempDir(),"permissions.db"));if err!=nil{t.Fatal(err)};defer s.Close();if err:=s.AddRoot(ctx,root);err!=nil{t.Fatal(err)};i:=Indexer{Store:s,Embedder:embedding.Projection{}};if _,err:=i.Reconcile(ctx);err!=nil{t.Fatal(err)};if err:=os.Remove(visible);err!=nil{t.Fatal(err)};if err:=os.Chmod(restricted,0);err!=nil{t.Fatal(err)};defer os.Chmod(restricted,0o700)
	stats,err:=i.Reconcile(ctx);if err==nil{t.Fatal("expected traversal error")};if stats.Deleted!=0{t.Fatalf("incomplete scan deleted stale documents: %+v",stats)};counts,countErr:=s.Counts(ctx);if countErr!=nil{t.Fatal(countErr)};if counts.Documents!=2||counts.Chunks!=2{t.Fatalf("incomplete scan damaged index: %+v",counts)}
}
