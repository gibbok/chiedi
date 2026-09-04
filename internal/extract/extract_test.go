package extract

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"strings"
)

func TestMarkdownHierarchyAndFence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "guide.md")
	content := "# Security\r\n\r\nIntro.\r\n\r\n## Sessions\r\nExpire later.\r\n\r\n```go\r\n# not a heading\r\n```\r\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := File(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "Security" || len(doc.Sections) != 2 {
		t.Fatalf("unexpected document: %#v", doc)
	}
	if doc.Sections[1].Heading != "Security > Sessions" {
		t.Fatalf("heading=%q", doc.Sections[1].Heading)
	}
}

func TestLongLineIsNotTruncated(t *testing.T){
	path:=filepath.Join(t.TempDir(),"long.txt");content:=strings.Repeat("x",128<<10);if err:=os.WriteFile(path,[]byte(content),0o600);err!=nil{t.Fatal(err)}
	doc,err:=File(context.Background(),path);if err!=nil{t.Fatal(err)};if len(doc.Sections)!=1||doc.Sections[0].Text!=content{t.Fatalf("long line was truncated: got %d bytes",len(doc.Sections[0].Text))}
}

func TestInvalidUTF8(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.txt")
	if err := os.WriteFile(path, []byte{0xff}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := File(context.Background(), path); err == nil {
		t.Fatal("expected invalid UTF-8 error")
	}
}
