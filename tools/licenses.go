//go:build ignore

// Licenses writes licenses/: the license texts of the code built into
// Candle besides its own, in the Windows and the macOS build, with a list of
// it in README.txt. Run it from the repository's root after go.mod changes:
//
//	go run tools/licenses.go
//
// mygo build ships the folder beside Candle.exe and in Candle.app's
// Resources (the resources of work/mygo.json).
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

var targets = []string{"darwin/arm64", "windows/amd64"}

type module struct {
	path, version, dir string
	pkgDirs            map[string]bool // of the packages built in
	targets            map[string]bool
}

func main() {
	if _, err := os.Stat("work/go.mod"); err != nil {
		check(fmt.Errorf("run it from the repository's root: %w", err))
	}
	mods := map[string]*module{}
	for _, t := range targets {
		goos, goarch, _ := strings.Cut(t, "/")
		out := goCmd([]string{"GOOS=" + goos, "GOARCH=" + goarch}, "list", "-tags", "mygo_noinspector", "-deps", "-f",
			"{{if .Module}}{{if not .Module.Main}}{{.Module.Path}}\t{{.Module.Version}}\t{{.Module.Dir}}\t{{.Dir}}{{end}}{{end}}", ".")
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			f := strings.Split(line, "\t")
			if len(f) != 4 {
				continue
			}
			m := mods[f[0]]
			if m == nil {
				m = &module{path: f[0], version: f[1], dir: f[2], pkgDirs: map[string]bool{}, targets: map[string]bool{}}
				mods[f[0]] = m
			}
			m.pkgDirs[f[3]] = true
			m.targets[t] = true
		}
	}

	check(os.RemoveAll("licenses"))
	var list strings.Builder
	list.WriteString("Candle 里编进程序的第三方代码按各自的许可证分发，原文都在这个文件夹里。\n")
	list.WriteString("Candle 自己的许可证（MIT）是上一层的 LICENSE。\n")
	list.WriteString("这份清单和许可证由 tools/licenses.go 生成。\n\n")

	goroot := strings.TrimSpace(goCmd(nil, "env", "GOROOT"))
	goversion := strings.TrimSpace(goCmd(nil, "env", "GOVERSION"))
	n := copyLicenses(goroot, goroot, "go")
	fmt.Fprintf(&list, "%-29s %-10s %s\n", "Go 标准库", goversion, "go/") // 标准库 is 6 columns wide, not 3
	if n == 0 {
		check(fmt.Errorf("no license in %s", goroot))
	}

	paths := make([]string, 0, len(mods))
	for p := range mods {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	for _, p := range paths {
		m := mods[p]
		// The module's own license, and those of the folders of the
		// packages built in, as code ported from elsewhere keeps its own.
		dirs := map[string]bool{m.dir: true}
		for d := range m.pkgDirs {
			for ; strings.HasPrefix(d, m.dir+string(filepath.Separator)); d = filepath.Dir(d) {
				dirs[d] = true
			}
		}
		n := 0
		for d := range dirs {
			rel, err := filepath.Rel(m.dir, d)
			check(err)
			n += copyLicenses(m.dir, d, filepath.Join(filepath.FromSlash(m.path), rel))
		}
		if n == 0 {
			check(fmt.Errorf("no license in %s %s", m.path, m.version))
		}
		only := ""
		if len(m.targets) < len(targets) {
			for t := range m.targets {
				only = "（只在 " + map[string]string{"darwin/arm64": "macOS", "windows/amd64": "Windows"}[t] + " 版里）"
			}
		}
		fmt.Fprintf(&list, "%-32s %-10s %s/%s\n", m.path, m.version, m.path, only)
	}
	check(os.WriteFile(filepath.Join("licenses", "README.txt"), []byte(list.String()), 0o644))
	fmt.Printf("licenses/: Go and %d modules\n", len(paths))
}

// copyLicenses copies the license files of dir, of the module at root, to
// licenses/to, and says how many.
func copyLicenses(root, dir, to string) int {
	entries, err := os.ReadDir(dir)
	check(err)
	n := 0
	for _, e := range entries {
		if !e.Type().IsRegular() || !isLicense(e.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		check(err)
		check(os.MkdirAll(filepath.Join("licenses", to), 0o755))
		check(os.WriteFile(filepath.Join("licenses", to, e.Name()), data, 0o644))
		n++
	}
	return n
}

func isLicense(name string) bool {
	n := strings.ToUpper(name)
	for _, p := range []string{"LICENSE", "LICENCE", "COPYING", "NOTICE", "PATENTS", "UNLICENSE"} {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

func goCmd(env []string, args ...string) string {
	cmd := exec.Command("go", args...)
	cmd.Dir = "work"
	cmd.Env = append(os.Environ(), env...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	check(err)
	return string(out)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
