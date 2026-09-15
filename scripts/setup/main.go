// Command setup prepares pinned native assets. Run from the repository root.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type asset struct {
	URL    string
	SHA256 string
}
type lockfile struct {
	ONNXRuntime     string
	Tokenizers      string
	Model           string
	Revision        string
	ModelID         string `json:"model_id"`
	ModelSHA256     string `json:"model_sha256"`
	TokenizerSHA256 string `json:"tokenizer_prepared_sha256"`
	Platforms       map[string]map[string]asset
}

func digest(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func download(client *http.Client, url, target, expected string) error {
	if sum, err := digest(target); err == nil && (expected == "" || sum == expected) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = downloadOnce(client, url, target, expected)
		if err == nil {
			return nil
		}
		if attempt < 2 {
			time.Sleep(time.Second)
		}
	}
	return err
}

func downloadOnce(client *http.Client, url, target, expected string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "chiedi-build")
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, response.Status)
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	_, err = io.Copy(temp, response.Body)
	closeErr := temp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if expected != "" {
		sum, err := digest(temp.Name())
		if err != nil {
			return err
		}
		if sum != expected {
			return fmt.Errorf("checksum mismatch: %s", url)
		}
	}
	return os.Rename(temp.Name(), target)
}

// Copy only explicitly selected regular files; archive paths are never extracted.
func extract(archive string, members map[string]string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	found := map[string]bool{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			continue
		}
		for suffix, destination := range members {
			if !strings.HasSuffix(h.Name, suffix) {
				continue
			}
			if found[suffix] {
				return fmt.Errorf("duplicate archive member: %s", suffix)
			}
			found[suffix] = true
			if err := writeFile(destination, tr); err != nil {
				return err
			}
		}
	}
	for suffix := range members {
		if !found[suffix] {
			return fmt.Errorf("missing archive member: %s", suffix)
		}
	}
	return nil
}

func writeFile(destination string, source io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	f, err := os.Create(destination)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, source)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func copyFile(source, destination string) error {
	f, err := os.Open(source)
	if err != nil {
		return err
	}
	defer f.Close()
	return writeFile(destination, f)
}

// Preserve upstream key order and number/string spelling for the pinned checksum.
func prepareTokenizer(raw []byte) ([]byte, error) {
	if !json.Valid(raw) {
		return nil, fmt.Errorf("invalid tokenizer JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("tokenizer must be an object")
	}
	var out bytes.Buffer
	out.WriteByte('{')
	seen := map[string]bool{}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, err
		}
		name := key.(string)
		if seen[name] {
			return nil, fmt.Errorf("duplicate tokenizer field: %s", name)
		}
		if len(seen) > 0 {
			out.WriteByte(',')
		}
		seen[name] = true
		encoded, _ := json.Marshal(name)
		out.Write(encoded)
		out.WriteByte(':')
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
		if name == "truncation" || name == "padding" {
			value = json.RawMessage("null")
		}
		if err := json.Compact(&out, value); err != nil {
			return nil, err
		}
	}
	for _, name := range []string{"truncation", "padding"} {
		if !seen[name] {
			if len(seen) > 0 {
				out.WriteByte(',')
			}
			fmt.Fprintf(&out, "%q:null", name)
			seen[name] = true
		}
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

func platformKey(targetOS, targetArch, hostOS, hostArch string, platforms map[string]map[string]asset) (string, error) {
	key := targetOS + "-" + targetArch
	if _, ok := platforms[key]; !ok || targetOS != hostOS || targetArch != hostArch {
		return "", fmt.Errorf("build natively on Linux/macOS amd64 or arm64; cross compilation is not configured")
	}
	return key, nil
}

func run() error {
	raw, err := os.ReadFile("scripts/native-assets.json")
	if err != nil {
		return err
	}
	var lock lockfile
	if err := json.Unmarshal(raw, &lock); err != nil {
		return err
	}
	// GOHOST* still identifies the machine when GOOS/GOARCH request a cross build.
	host, err := exec.Command("go", "env", "GOHOSTOS", "GOHOSTARCH").Output()
	if err != nil {
		return err
	}
	fields := strings.Fields(string(host))
	if len(fields) != 2 {
		return fmt.Errorf("cannot determine Go host platform")
	}
	key, err := platformKey(runtime.GOOS, runtime.GOARCH, fields[0], fields[1], lock.Platforms)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 180 * time.Second}
	assets := "bin/assets"
	notices := assets + "/licenses"
	library := "libonnxruntime.so"
	suffix := "/lib/libonnxruntime.so." + lock.ONNXRuntime
	if runtime.GOOS == "darwin" {
		library = "libonnxruntime.dylib"
		suffix = "/lib/libonnxruntime." + lock.ONNXRuntime + ".dylib"
	}
	for _, name := range []string{"ort", "tokenizer"} {
		config := lock.Platforms[key][name]
		archive := ".deps/downloads/" + path.Base(config.URL)
		if err := download(client, config.URL, archive, config.SHA256); err != nil {
			return err
		}
		members := map[string]string{"libtokenizers.a": ".deps/lib/libtokenizers.a"}
		if name == "ort" {
			members = map[string]string{
				"/include/onnxruntime_c_api.h":    ".deps/include/onnxruntime_c_api.h",
				"/include/onnxruntime_ep_c_api.h": ".deps/include/onnxruntime_ep_c_api.h",
				suffix:                            assets + "/" + library,
				"/LICENSE":                        notices + "/onnxruntime-LICENSE",
				"/ThirdPartyNotices.txt":          notices + "/onnxruntime-ThirdPartyNotices.txt",
			}
		}
		if err := extract(archive, members); err != nil {
			return err
		}
	}
	base := "https://huggingface.co/" + lock.Model + "/resolve/" + lock.Revision + "/"
	model := ".deps/downloads/e5-" + lock.Revision + "-int8.onnx"
	if err := download(client, base+"onnx/model_qint8_avx512_vnni.onnx", model, lock.ModelSHA256); err != nil {
		return err
	}
	if err := copyFile(model, assets+"/model.onnx"); err != nil {
		return err
	}
	tokenizer := ".deps/downloads/e5-" + lock.Revision + "-tokenizer.json"
	if err := download(client, base+"tokenizer.json", tokenizer, ""); err != nil {
		return err
	}
	raw, err = os.ReadFile(tokenizer)
	if err != nil {
		return err
	}
	prepared, err := prepareTokenizer(raw)
	if err != nil {
		return err
	}
	if fmt.Sprintf("%x", sha256.Sum256(prepared)) != lock.TokenizerSHA256 {
		return fmt.Errorf("prepared tokenizer checksum mismatch; remove cached tokenizer and run setup again")
	}
	if err := writeFile(assets+"/tokenizer.json", bytes.NewReader(prepared)); err != nil {
		return err
	}
	for name, url := range map[string]string{
		"tokenizers-LICENSE":             "https://raw.githubusercontent.com/daulet/tokenizers/v1.27.0/LICENSE",
		"e5-MIT-LICENSE":                 "https://raw.githubusercontent.com/microsoft/unilm/master/LICENSE",
		"huggingface-tokenizers-LICENSE": "https://raw.githubusercontent.com/huggingface/tokenizers/v0.22.2/LICENSE",
	} {
		if err := download(client, url, notices+"/"+name, ""); err != nil {
			return err
		}
	}
	files := map[string]string{}
	for _, name := range []string{"model.onnx", "tokenizer.json", library} {
		files[name], err = digest(assets + "/" + name)
		if err != nil {
			return err
		}
	}
	manifest := map[string]any{"model_id": lock.ModelID, "model_source": base, "onnxruntime": lock.ONNXRuntime, "tokenizers": lock.Tokenizers, "platform": key, "files": files}
	raw, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(assets+"/manifest.json", append(raw, '\n'), 0644); err != nil {
		return err
	}
	fmt.Println("Native E5 assets ready: " + assets)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "setup:", err)
		os.Exit(1)
	}
}
