package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownload(t *testing.T) {
	payload := "pinned asset"
	sum := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/bad" {
			http.Error(w, "failed", 500)
			return
		}
		fmt.Fprint(w, payload)
	}))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "asset")
	for _, content := range []string{"", "corrupt"} {
		if err := os.WriteFile(target, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		if err := download(server.Client(), server.URL, target, sum); err != nil {
			t.Fatal(err)
		}
	}
	before := calls
	if err := download(server.Client(), server.URL, target, sum); err != nil {
		t.Fatal(err)
	}
	if calls != before {
		t.Fatal("cache was downloaded again")
	}
	for _, url := range []string{server.URL, server.URL + "/bad"} {
		if err := downloadOnce(server.Client(), url, target, strings.Repeat("0", 64)); err == nil {
			t.Fatal("accepted failed download")
		}
		raw, _ := os.ReadFile(target)
		if string(raw) != payload {
			t.Fatal("replaced valid target on failure")
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(target))
	if len(entries) != 1 {
		t.Fatal("temporary download leaked")
	}
}

func TestDownloadRejectsUntrustedTLS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "untrusted") }))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "asset")
	if err := downloadOnce(http.DefaultClient, server.URL, target, ""); err == nil {
		t.Fatal("accepted untrusted certificate")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("published untrusted content")
	}
}

func TestPrepareTokenizer(t *testing.T) {
	raw := []byte(`{ "version":"1.0", "truncation":{"max_length":512}, "padding":{}, "model":{"vocab":[["é<>数据库",-1.5]]} }`)
	got, err := prepareTokenizer(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"version":"1.0","truncation":null,"padding":null,"model":{"vocab":[["é<>数据库",-1.5]]}}`
	if string(got) != want {
		t.Fatalf("got %s", got)
	}
	for _, raw := range []string{`[]`, `{"x":1,"x":2}`, `{`} {
		if _, err := prepareTokenizer([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestExtract(t *testing.T) {
	for _, scenario := range []string{"regular", "symlink", "missing", "duplicate"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			var data bytes.Buffer
			gz := gzip.NewWriter(&data)
			tw := tar.NewWriter(gz)
			header := &tar.Header{Name: "package/lib/libexample.a", Mode: 0644, Size: 2, Typeflag: tar.TypeReg}
			if scenario == "symlink" {
				header.Typeflag = tar.TypeSymlink
				header.Linkname = "/etc/passwd"
				header.Size = 0
			}
			if scenario == "missing" {
				header.Name = "other"
			}
			count := 1
			if scenario == "duplicate" {
				count = 2
			}
			for i := 0; i < count; i++ {
				if err := tw.WriteHeader(header); err != nil {
					t.Fatal(err)
				}
				if header.Size > 0 {
					if _, err := tw.Write([]byte("ok")); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			archive := filepath.Join(dir, "test.tgz")
			if err := os.WriteFile(archive, data.Bytes(), 0644); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(dir, "out/lib.a")
			err := extract(archive, map[string]string{"libexample.a": target})
			if scenario == "regular" {
				if err != nil {
					t.Fatal(err)
				}
				raw, _ := os.ReadFile(target)
				if string(raw) != "ok" {
					t.Fatal("bad extracted content")
				}
			} else if err == nil {
				t.Fatal("accepted invalid archive")
			}
		})
	}
}

func TestPlatforms(t *testing.T) {
	raw, err := os.ReadFile("../native-assets.json")
	if err != nil {
		t.Fatal(err)
	}
	var lock lockfile
	if err := json.Unmarshal(raw, &lock); err != nil {
		t.Fatal(err)
	}
	for _, platform := range []string{"linux-amd64", "linux-arm64", "darwin-amd64", "darwin-arm64"} {
		parts := strings.Split(platform, "-")
		if _, err := platformKey(parts[0], parts[1], parts[0], parts[1], lock.Platforms); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := platformKey("darwin", "arm64", "linux", "amd64", lock.Platforms); err == nil {
		t.Fatal("accepted cross build")
	}
	if _, err := platformKey("windows", "amd64", "windows", "amd64", lock.Platforms); err == nil {
		t.Fatal("accepted unsupported host")
	}
}
