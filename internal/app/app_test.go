package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type failingWriter struct{}
func (failingWriter)Write([]byte)(int,error){return 0,errors.New("write failed")}

func TestUnknownCommandDoesNotCreateDatabase(t *testing.T){
	path:=filepath.Join(t.TempDir(),"must-not-exist.db");var stdout,stderr bytes.Buffer
	if code:=Run(context.Background(),[]string{"--db",path,"typo"},bytes.NewReader(nil),&stdout,&stderr);code!=2{t.Fatalf("exit code=%d stderr=%s",code,stderr.String())}
	if _,err:=os.Stat(path);!os.IsNotExist(err){t.Fatalf("unknown command created database: %v",err)}
}

func TestIndexReportsOutputFailure(t *testing.T){
	path:=filepath.Join(t.TempDir(),"output.db");var stderr bytes.Buffer
	if code:=Run(context.Background(),[]string{"--db",path,"index"},bytes.NewReader(nil),failingWriter{},&stderr);code!=1{t.Fatalf("exit code=%d stderr=%s",code,stderr.String())}
	if !bytes.Contains(stderr.Bytes(),[]byte("write failed")){t.Fatalf("stderr=%s",stderr.String())}
}
