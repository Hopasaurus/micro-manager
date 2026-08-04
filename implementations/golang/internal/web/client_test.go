package web

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestClientScripts runs the mm.js behaviour suite (T-0111, Option B).
//
// mm.js is the largest untested surface in the service: everything else has
// tests, the client script had source-shape greps. The suite in jstest/
// drives the REAL script against a jsdom document and a double for htmx under
// `node --test`, asserting the data-* attributes and the §7.3 effects the
// script is supposed to maintain — not its internals.
//
// It is skipped when node or the jsdom dependency is absent, exactly as the
// check.sh cross-check skips when bash is missing (AGENTS.md): a missing
// toolchain must not fail the Go build, but a present one must run everything.
// A suite that silently skipped is not a suite, so the TAP summary is checked
// too: it must report that tests RAN, not just that nothing failed.
func TestClientScripts(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed; skipping the mm.js suite (internal/web/jstest)")
	}
	if _, err := os.Stat(filepath.Join("jstest", "node_modules", "jsdom")); err != nil {
		t.Skip("jsdom is not installed (run `npm install` in internal/web/jstest); skipping the mm.js suite")
	}

	cmd := exec.Command(node, "--test", "--test-reporter=tap", "mm.test.js")
	cmd.Dir = "jstest"
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mm.js suite failed:\n%s", out)
	}

	// TAP reports "# tests N" then "# pass N" then "# fail N". A suite that
	// matched nothing reports "# tests 0", which a zero exit code would let
	// through.
	if !bytes.Contains(out, []byte("# tests ")) {
		t.Fatalf("mm.js suite produced no TAP summary; it did not run:\n%s", out)
	}
}
