package mm

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The formatting guard (T-0163).
//
// AGENTS.md's style standard is "gofmt; go vet clean", and nothing enforced
// it, so five files in this package drifted — alignment and comment
// indentation only, zero behavior change — and the drift was only noticed
// while shipping T-0040. A future edit to any of those files would then have
// reformatted it as noise in a diff that should only contain the edit.
//
// Like requireBash and TestClientScripts, this shells out to a tool and skips
// when the tool is absent: a missing toolchain must not fail the Go build,
// but a present one must run everything. `gofmt -l` prints nothing for a
// clean tree, which is indistinguishable from the guard never running, so the
// check also proves it ran (modulePackages) and a companion test points the
// same invocation at a deliberately unformatted file and requires it to be
// named (TestGofmtDetectsDrift).

// requireGofmt returns the gofmt binary, skipping when it is not on PATH.
func requireGofmt(t *testing.T) string {
	t.Helper()
	gofmt, err := exec.LookPath("gofmt")
	if err != nil {
		t.Skip("gofmt is not on PATH; skipping the formatting guard")
	}
	return gofmt
}

// moduleRoot resolves the module root the way the tooling documents it: `go
// env GOMOD` prints the absolute path of the go.mod file, whose directory is
// the module root. The test runs from this package's directory, which may be
// anywhere under the module, so the root is resolved rather than assumed.
func moduleRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	gomod := strings.TrimSpace(string(out))
	// Outside a module, GOMOD prints "/dev/null" — and the guard must fail
	// loudly then, because it would otherwise check nothing.
	if gomod == "" || gomod == "/dev/null" {
		t.Fatalf("go env GOMOD printed %q; the test is not inside the module", gomod)
	}
	if _, err := os.Stat(gomod); err != nil {
		t.Fatalf("go env GOMOD printed %s, which does not exist: %v", gomod, err)
	}
	return filepath.Dir(gomod)
}

// modulePackages lists every package directory in the module as an absolute
// path, via `go list -f '{{.Dir}}' ./...` run from the module root.
//
// The cwd matters: `./...` is relative to it, and a test binary runs from its
// own package directory — from mm/ it would match only mm, and the guard
// would quietly check a fifth of the module. The root resolved by moduleRoot
// is what makes the enumeration module-wide.
func modulePackages(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.Command("go", "list", "-f", "{{.Dir}}", "./...")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -f '{{.Dir}}' ./...: %v", err)
	}
	var dirs []string
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			dirs = append(dirs, line)
		}
	}
	// A guard on the guard: if the module is ever restructured so that this
	// finds nothing, the test must fail rather than pass vacuously.
	if len(dirs) == 0 {
		t.Fatal("go list ./... returned no packages; the guard checked nothing")
	}
	return dirs
}

// TestGofmtClean fails naming every file gofmt would reformat, across every
// package in the module.
func TestGofmtClean(t *testing.T) {
	gofmt := requireGofmt(t)
	root := moduleRoot(t)
	dirs := modulePackages(t, root)
	t.Logf("checking %d packages under %s", len(dirs), root)

	out, err := exec.Command(gofmt, append([]string{"-l"}, dirs...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("gofmt -l: %v\n%s", err, out)
	}
	if dirty := strings.TrimSpace(string(out)); dirty != "" {
		t.Errorf("gofmt -l reports unformatted files; run `gofmt -w` on them:\n%s", dirty)
	}
}

// TestGofmtDetectsDrift is the guard on the guard: a clean tree makes gofmt
// print nothing, exactly what a broken invocation would print, so the same
// gofmt is pointed at a deliberately unformatted file and must name it.
func TestGofmtDetectsDrift(t *testing.T) {
	gofmt := requireGofmt(t)

	dir := t.TempDir()
	dirty := filepath.Join(dir, "dirty.go")
	if err := os.WriteFile(dirty, []byte("package p\nfunc f() {\nif true {\n_ = 1\n}\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(gofmt, "-l", dir).CombinedOutput()
	if err != nil {
		t.Fatalf("gofmt -l on a deliberately unformatted file: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "dirty.go") {
		t.Errorf("gofmt -l did not report the deliberately unformatted file; output:\n%s", out)
	}
}
