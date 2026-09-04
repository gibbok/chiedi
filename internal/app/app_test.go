package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestUnknownCommandDoesNotCreateDatabase(t *testing.T){
	path:=filepath.Join(t.TempDir(),"must-not-exist.db");var stdout,stderr bytes.Buffer
	if code:=Run(context.Background(),[]string{"--db",path,"typo"},bytes.NewReader(nil),&stdout,&stderr);code!=2{t.Fatalf("exit code=%d stderr=%s",code,stderr.String())}
	if _,err:=os.Stat(path);!os.IsNotExist(err){t.Fatalf("unknown command created database: %v",err)}
}
