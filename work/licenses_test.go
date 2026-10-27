package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// licenses/, which ships with the app, has every module built into the
// Windows and the macOS build, at the version go.mod takes. After changing
// go.mod, run go run tools/licenses.go from the repository's root.
func TestLicensesHaveEveryModule(t *testing.T) {
	list, err := os.ReadFile("../licenses/README.txt")
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, line := range strings.Split(string(list), "\n") {
		if f := strings.Fields(line); len(f) >= 2 {
			listed[f[0]+" "+f[1]] = true
		}
	}
	for _, target := range []string{"darwin/arm64", "windows/amd64"} {
		goos, goarch, _ := strings.Cut(target, "/")
		cmd := exec.Command("go", "list", "-tags", "mygo_noinspector", "-deps", "-f", "{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}}{{end}}{{end}}", ".")
		cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list for %s: %v", target, err)
		}
		for _, m := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if m == "" || listed[m] {
				continue
			}
			path, _, _ := strings.Cut(m, " ")
			if files, _ := os.ReadDir(filepath.Join("..", "licenses", filepath.FromSlash(path))); len(files) == 0 {
				t.Errorf("%s (%s) has no license in licenses/: go run tools/licenses.go", m, target)
			} else {
				t.Errorf("%s (%s) is not in licenses/README.txt at that version: go run tools/licenses.go", m, target)
			}
			listed[m] = true // once
		}
	}
	for _, f := range []string{"go/LICENSE", "../LICENSE"} {
		if _, err := os.Stat(filepath.Join("..", "licenses", f)); err != nil {
			t.Error(err)
		}
	}
}
