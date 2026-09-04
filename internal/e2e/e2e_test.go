package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBuiltBinaryCommonUseCase(t *testing.T){
	if testing.Short(){t.Skip("end-to-end test excluded only by explicit -short")}
	_,file,_,_:=runtime.Caller(0);repo:=filepath.Clean(filepath.Join(filepath.Dir(file),"..",".."));dir:=t.TempDir();binary:=filepath.Join(dir,"docdex")
	cmd:=exec.Command("go","build","-o",binary,"./cmd/docdex");cmd.Dir=repo;if out,err:=cmd.CombinedOutput();err!=nil{t.Fatalf("build: %v\n%s",err,out)}
	corpus:=filepath.Join(dir,"acceptance");if err:=os.MkdirAll(filepath.Join(corpus,"nested"),0o700);err!=nil{t.Fatal(err)}
	write(t,filepath.Join(corpus,"hr.md"),"# Employee handbook\n\n## Annual leave\n\nEmployees are entitled to twenty business days of paid annual leave each year.\n")
	write(t,filepath.Join(corpus,"technical.txt"),"The storage engine uses write-ahead logging. Exact reference DB-ZX-481.\n")
	write(t,filepath.Join(corpus,"nested","project.md"),"# Project Aurora\n\nThe release train leaves on Friday.\n")
	write(t,filepath.Join(corpus,"ignored.bin"),"annual leave distractor")
	writePDF(t,filepath.Join(corpus,"contract.pdf"),"Either party may end the agreement early with thirty days written notice. Ref CT-902.")
	db:=filepath.Join(dir,"docdex.db");run:=func(args ...string)string{t.Helper();all:=append([]string{"--db",db},args...);c:=exec.Command(binary,all...);out,err:=c.CombinedOutput();if err!=nil{t.Fatalf("%v: %v\n%s",args,err,out)};return string(out)}
	run("init");run("add",corpus);indexOut:=run("index");if !strings.Contains(indexOut,`"embedding_jobs": 4`){t.Fatalf("unexpected initial index:\n%s",indexOut)}
	status:=run("status");if !strings.Contains(status,`"indexed_count": 4`)||strings.Contains(status,"ignored.bin"){t.Fatalf("unexpected status:\n%s",status)}
	semantic:=run("search","How many vacation days do workers get?");if !strings.Contains(strings.SplitN(semantic,"\n\n",2)[0],"hr.md")||!strings.Contains(semantic,"twenty business days"){t.Fatalf("semantic miss:\n%s",semantic)}
	exact:=run("search","DB-ZX-481");if !strings.Contains(strings.SplitN(exact,"\n\n",2)[0],"technical.txt"){t.Fatalf("exact miss:\n%s",exact)}
	contract:=run("search","Can the contractor terminate the contract ahead of time?");if !strings.Contains(contract,"contract.pdf"){t.Fatalf("PDF semantic miss:\n%s",contract)}
	testMCP(t,binary,db)
	writeAt(t,filepath.Join(corpus,"hr.md"),"# Employee handbook\n\n## Annual leave\n\nEmployees are entitled to twenty-five business days of paid annual leave each year.\n",time.Now().Add(2*time.Second));if got:=run("search","vacation allowance");!strings.Contains(got,"twenty-five"){t.Fatalf("search did not reconcile updated content:\n%s",got)};statusAfterSearch:=run("status");if !strings.Contains(statusAfterSearch,`"embedding_jobs": 1`){t.Fatalf("search did not embed exactly one changed chunk:\n%s",statusAfterSearch)}
	if err:=os.Rename(filepath.Join(corpus,"technical.txt"),filepath.Join(corpus,"system.txt"));err!=nil{t.Fatal(err)};renamed:=run("reconcile");if !strings.Contains(renamed,`"embedding_jobs": 0`)||!strings.Contains(renamed,`"renamed_documents": 1`){t.Fatalf("rename re-embedded:\n%s",renamed)}
	if err:=os.Remove(filepath.Join(corpus,"nested","project.md"));err!=nil{t.Fatal(err)};deleted:=run("reconcile");if !strings.Contains(deleted,`"deleted_documents": 1`){t.Fatalf("delete not reconciled:\n%s",deleted)}
	unchanged:=run("reconcile");if !strings.Contains(unchanged,`"extraction_jobs": 0`)||!strings.Contains(unchanged,`"embedding_jobs": 0`){t.Fatalf("idempotence failed:\n%s",unchanged)}
	write(t,filepath.Join(corpus,"broken.pdf"),"malformed");bad:=run("reconcile");if !strings.Contains(bad,`"failed_documents": 1`){t.Fatalf("bad PDF not isolated:\n%s",bad)}
	if got:=run("search","How much annual time off?");!strings.Contains(got,"hr.md"){t.Fatalf("restart retrieval failed:\n%s",got)}
	if got:=run("doctor");!strings.HasPrefix(got,"ok:"){t.Fatalf("doctor failed: %s",got)}
}

func testMCP(t *testing.T,binary,db string){
	t.Helper();cmd:=exec.Command(binary,"--db",db,"mcp");stdin,err:=cmd.StdinPipe();if err!=nil{t.Fatal(err)};stdout,err:=cmd.StdoutPipe();if err!=nil{t.Fatal(err)};var stderr bytes.Buffer;cmd.Stderr=&stderr;if err:=cmd.Start();err!=nil{t.Fatal(err)}
	requests:=[]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"retrieve","arguments":{"question":"vacation allowance","limit":3}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"read_chunks","arguments":{"chunk_ids":[1],"before":0,"after":1}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"list_documents","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"index_status","arguments":{}}}`,
	}
	for _,line:=range requests{fmt.Fprintln(stdin,line)};stdin.Close();scanner:=bufio.NewScanner(stdout);var responses []map[string]any;for scanner.Scan(){var response map[string]any;if err:=json.Unmarshal(scanner.Bytes(),&response);err!=nil{t.Fatalf("MCP stdout is not JSON: %q",scanner.Text())};responses=append(responses,response)}
	if err:=cmd.Wait();err!=nil{t.Fatalf("MCP exit: %v stderr=%s",err,stderr.String())};if len(responses)!=len(requests){t.Fatalf("MCP responses=%d want=%d stderr=%s",len(responses),len(requests),stderr.String())}
	for n,response:=range responses{if response["error"]!=nil{t.Fatalf("MCP response %d: %+v",n,response)}}
	encoded,_:=json.Marshal(responses);text:=string(encoded);for _,required:=range []string{"retrieve","read_chunks","list_documents","index_status","hr.md"}{if !strings.Contains(text,required){t.Fatalf("MCP output missing %q: %s",required,text)}}
}

func write(t *testing.T,path,content string){t.Helper();if err:=os.WriteFile(path,[]byte(content),0o600);err!=nil{t.Fatal(err)}}
func writeAt(t *testing.T,path,content string,mtime time.Time){t.Helper();write(t,path,content);if err:=os.Chtimes(path,mtime,mtime);err!=nil{t.Fatal(err)}}

func writePDF(t *testing.T,path,text string){
	t.Helper();escaped:=strings.NewReplacer("\\","\\\\","(","\\(",")","\\)").Replace(text);stream:="BT /F1 12 Tf 72 720 Td ("+escaped+") Tj ET";objects:=[]string{
		"<< /Type /Catalog /Pages 2 0 R >>","<< /Type /Pages /Kids [3 0 R] /Count 1 >>","<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream",len(stream),stream),"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	};var b bytes.Buffer;b.WriteString("%PDF-1.4\n");offsets:=[]int{0};for n,obj:=range objects{offsets=append(offsets,b.Len());fmt.Fprintf(&b,"%d 0 obj\n%s\nendobj\n",n+1,obj)};xref:=b.Len();fmt.Fprintf(&b,"xref\n0 %d\n0000000000 65535 f \n",len(objects)+1);for _,off:=range offsets[1:]{fmt.Fprintf(&b,"%010d 00000 n \n",off)};fmt.Fprintf(&b,"trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",len(objects)+1,xref);if err:=os.WriteFile(path,b.Bytes(),0o600);err!=nil{t.Fatal(err)}
}
