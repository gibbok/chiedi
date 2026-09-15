package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func TestInstaller(t *testing.T) {
	installer, err := filepath.Abs("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"relative spaces", "shared permissions", "running upgrade", "legacy upgrade", "failed copy"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "source")
			prefix := filepath.Join(root, "installed")
			write(t, filepath.Join(source, "chiedi"), "#!/bin/sh\nexec sleep \"${1:-0}\"\n", 0700)
			write(t, filepath.Join(source, "assets/manifest.json"), `{"version": 1}`, 0600)
			install := func(fakePath string) error {
				cmd := exec.Command("bash", installer, source)
				cmd.Dir = root
				cmd.Env = append(os.Environ(), "PREFIX="+prefix)
				if fakePath != "" {
					cmd.Env = append(cmd.Env, "PATH="+fakePath+string(os.PathListSeparator)+os.Getenv("PATH"))
				}
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Log(string(out))
				}
				return err
			}
			if scenario == "relative spaces" {
				prefix = "relative install"
			}
			binary := filepath.Join(prefix, "bin/chiedi")
			if !filepath.IsAbs(binary) {
				binary = filepath.Join(root, binary)
			}
			resolve := func() string {
				t.Helper()
				p, err := filepath.EvalSymlinks(binary)
				if err != nil {
					t.Fatal(err)
				}
				return p
			}
			run := func() {
				t.Helper()
				if out, err := exec.Command(binary, "0").CombinedOutput(); err != nil {
					t.Fatalf("%v: %s", err, out)
				}
			}
			if scenario == "legacy upgrade" {
				release := filepath.Join(prefix, "lib/chiedi")
				write(t, filepath.Join(release, "chiedi"), "#!/bin/sh\nexec sleep \"${1:-0}\"\n", 0755)
				write(t, filepath.Join(release, "assets/manifest.json"), `{"version": 1}`, 0644)
				if err := os.MkdirAll(filepath.Dir(binary), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(release, "chiedi"), binary); err != nil {
					t.Fatal(err)
				}
			} else if err := install(""); err != nil {
				t.Fatal(err)
			}
			old := resolve()
			switch scenario {
			case "relative spaces":
				link, err := os.Readlink(binary)
				if err != nil || !filepath.IsAbs(link) {
					t.Fatalf("link %q: %v", link, err)
				}
			case "shared permissions":
				for _, p := range []string{filepath.Dir(old), old, filepath.Join(filepath.Dir(old), "assets")} {
					info, err := os.Stat(p)
					if err != nil {
						t.Fatal(err)
					}
					if info.Mode().Perm()&0055 != 0055 {
						t.Fatalf("unreadable: %s", p)
					}
				}
				info, err := os.Stat(filepath.Join(filepath.Dir(old), "assets/manifest.json"))
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm()&0044 != 0044 {
					t.Fatal("unreadable manifest")
				}
			case "running upgrade", "legacy upgrade":
				process := exec.Command(binary, "60")
				if err := process.Start(); err != nil {
					t.Fatal(err)
				}
				done := make(chan error, 1)
				go func() { done <- process.Wait() }()
				defer func() { _ = process.Process.Kill(); <-done }()
				write(t, filepath.Join(source, "assets/manifest.json"), `{"version": 2}`, 0644)
				if err := install(""); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-done:
					done <- err
					t.Fatalf("upgrade stopped active process: %v", err)
				default:
				}
				if resolve() == old {
					t.Fatal("release did not change")
				}
				for p, want := range map[string]string{old: `{"version": 1}`, resolve(): `{"version": 2}`} {
					data, err := os.ReadFile(filepath.Join(filepath.Dir(p), "assets/manifest.json"))
					if err != nil || string(data) != want {
						t.Fatalf("asset %s: %s %v", p, data, err)
					}
				}
			case "failed copy":
				releases := filepath.Join(prefix, "lib/chiedi/releases")
				names := func() []string {
					t.Helper()
					entries, err := os.ReadDir(releases)
					if err != nil {
						t.Fatal(err)
					}
					var result []string
					for _, entry := range entries {
						result = append(result, entry.Name())
					}
					return result
				}
				before := names()
				fake := filepath.Join(root, "fake-bin")
				cp, err := exec.LookPath("cp")
				if err != nil {
					t.Fatal(err)
				}
				write(t, filepath.Join(fake, "cp"), "#!/bin/sh\nif [ \"$1\" = \"-R\" ]; then exit 19; fi\nexec \""+cp+"\" \"$@\"\n", 0755)
				if err := install(fake); err == nil {
					t.Fatal("expected copy failure")
				}
				if resolve() != old || !reflect.DeepEqual(before, names()) {
					t.Fatal("failed copy changed installation")
				}
			}
			run()
		})
	}
}
