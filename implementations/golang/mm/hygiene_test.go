package mm

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The library is a guest in a process it does not own (spec-tools.md §2.2).
//
// It is compiled into the CLI *and* into the UI service. A library that prints
// corrupts a --json envelope; one that calls os.Exit takes down a server
// handling other requests; one that reads the environment makes a long-lived
// service silently inherit the shell of whoever started it, which is the exact
// coupling §3.5 exists to prevent.
//
// Review does not catch this reliably - every one of these calls looks harmless
// at the point it is written. Enforcing it mechanically does.
//
// The sources are PARSED rather than grepped, so that the rule is about calls
// and not about text: this file names every banned identifier, and the prose
// above says "os.Exit" twice, neither of which should trip anything.

// streamWriters are the two destinations §2.2 actually forbids. fmt.Fprintf is
// NOT banned outright: formatting into a strings.Builder is how the error
// taxonomy composes its messages, and that writes to nobody. What matters is
// the destination, so Fprint* is judged by its first argument.
var streamWriters = map[string]bool{"os.Stdout": true, "os.Stderr": true}

// banned maps package.identifier to why it may not appear in the library.
var banned = map[string]string{
	"os.Exit":      "the caller owns the process; return an error instead",
	"os.Getenv":    "the wrapper resolves the environment into parameters (§3.5)",
	"os.LookupEnv": "the wrapper resolves the environment into parameters (§3.5)",
	"os.Args":      "argv belongs to the command line wrapper",
	"os.Getwd":     "the caller supplies paths; the library must not read the cwd",

	"fmt.Print":   "return diagnostics, never print them",
	"fmt.Printf":  "return diagnostics, never print them",
	"fmt.Println": "return diagnostics, never print them",
	"print":       "return diagnostics, never print them",
	"println":     "return diagnostics, never print them",

	"log.Print":   "the library has no logger and no output stream",
	"log.Printf":  "the library has no logger and no output stream",
	"log.Println": "the library has no logger and no output stream",
	"log.Fatal":   "the caller owns the process; return an error instead",
	"log.Fatalf":  "the caller owns the process; return an error instead",
	"log.Fatalln": "the caller owns the process; return an error instead",
	"log.Panic":   "an expected failure is an error value, not a panic",

	"panic": "an expected failure is an error value, not a panic",

	"signal.Notify": "the library must not install signal handlers",
}

// libraryFiles lists the non-test sources of this package.
func libraryFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		out = append(out, name)
	}
	// A guard on the guard: if the package is ever restructured so that this
	// finds nothing, the test must fail rather than pass vacuously.
	if len(out) < 10 {
		t.Fatalf("only found %d library files (%v); the scan is probably wrong", len(out), out)
	}
	return out
}

// scanFile reports every §2.2 violation in one parsed source file.
func scanFile(fset *token.FileSet, file *ast.File) []string {
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident := calledName(call.Fun)
		if ident == "" {
			return true
		}
		if why, bad := banned[ident]; bad {
			out = append(out, fmt.Sprintf("%s: calls %s — %s",
				fset.Position(call.Pos()), ident, why))
		}
		if strings.HasPrefix(ident, "fmt.Fprint") && len(call.Args) > 0 {
			if dest := calledName(call.Args[0]); streamWriters[dest] {
				out = append(out, fmt.Sprintf("%s: writes to %s — return diagnostics, never print them",
					fset.Position(call.Pos()), dest))
			}
		}
		return true
	})
	return out
}

func TestLibraryDoesNotOwnTheProcess(t *testing.T) {
	fset := token.NewFileSet()

	for _, name := range libraryFiles(t) {
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, finding := range scanFile(fset, file) {
			t.Error(finding)
		}
	}
}

// The guard on the guard. A hygiene test that has quietly stopped detecting
// anything reports exactly what a clean library reports, so the detector is
// pointed at source that is deliberately full of violations.
func TestHygieneScanDetectsViolations(t *testing.T) {
	const dirty = `package p

import (
	"fmt"
	"os"
	"strings"
)

func bad() {
	os.Exit(1)
	_ = os.Getenv("MM_DIR")
	_, _ = os.Getwd()
	fmt.Println("hello")
	fmt.Fprintf(os.Stderr, "oh no")
	panic("unreachable")
}

func fine() string {
	var b strings.Builder
	fmt.Fprintf(&b, "composing a message is not printing")
	_, _ = os.ReadFile("backlog.md")
	_, _ = os.Stat("done.md")
	return b.String()
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "dirty.go", dirty, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := strings.Join(scanFile(fset, file), "\n")

	for _, want := range []string{
		"os.Exit", "os.Getenv", "os.Getwd", "fmt.Println", "os.Stderr", "panic",
	} {
		if !strings.Contains(found, want) {
			t.Errorf("the scan missed %s:\n%s", want, found)
		}
	}
	// And it must not flag the legitimate uses, or the rule would be unusable.
	for _, unwanted := range []string{"os.ReadFile", "os.Stat", "&b"} {
		if strings.Contains(found, unwanted) {
			t.Errorf("the scan wrongly flagged %s:\n%s", unwanted, found)
		}
	}
	if n := len(scanFile(fset, file)); n != 6 {
		t.Errorf("found %d violations, want 6:\n%s", n, found)
	}
}

// calledName renders the callee as "pkg.Name" or "Name", and "" for anything
// else (a method on a value, a function literal, an interface call).
func calledName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		pkg, ok := f.X.(*ast.Ident)
		if !ok {
			return ""
		}
		return pkg.Name + "." + f.Sel.Name
	}
	return ""
}

// The library must also not import packages whose whole purpose is owning the
// process. This catches a call form the AST walk above would miss, such as a
// banned function stored in a variable first.
func TestLibraryImports(t *testing.T) {
	forbidden := map[string]string{
		"log":            "the library has no logger",
		"os/signal":      "the library must not install signal handlers",
		"os/exec":        "the library does not run programs",
		"flag":           "argv belongs to the command line wrapper",
		"text/tabwriter": "rendering belongs to a front end",
	}
	fset := token.NewFileSet()

	for _, name := range libraryFiles(t) {
		file, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if why, bad := forbidden[path]; bad {
				t.Errorf("%s imports %q — %s", name, path, why)
			}
		}
	}
}

// filepath.Abs is permitted, and is the one place the library touches ambient
// process state: on a relative path it resolves against the working directory.
//
// It is deliberate and narrow - it converts a path the CALLER supplied, rather
// than discovering which directory to act on - but it does mean a relative path
// means different things to two processes with different working directories.
// Front ends should pass absolute paths; this test pins the fact that only Open
// and the discovery walker rely on it, so the exposure cannot spread quietly.
func TestWorkingDirectoryExposureIsContained(t *testing.T) {
	fset := token.NewFileSet()
	users := map[string]bool{}

	for _, name := range libraryFiles(t) {
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok && calledName(call.Fun) == "filepath.Abs" {
				users[filepath.Base(name)] = true
			}
			return true
		})
	}

	allowed := map[string]bool{
		"store.go":    true, // Open, on the path it is handed
		"discover.go": true, // canonical(), for deduplication
		"init.go":     true, // Init, on the path it is handed
	}
	for name := range users {
		if !allowed[name] {
			t.Errorf("%s calls filepath.Abs; relative-path resolution is meant to be "+
				"confined to Open, Init and discovery — see spec-tools.md §2.2 rule 4", name)
		}
	}
}
