package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// T-0202 — the capability list (spec-tools.md §3.4.1).
//
// Most of the surface is optional (§5.2, §5.3), so a caller cannot assume an
// operation is there. The three ways to find out all worked before this and
// none was structured: read a page written for a human, exit-code-probe one
// operation at a time, or run it and interpret exit 2. What these tests pin is
// that the structured answer is TRUE — derived from the parser, not from the
// prose, because two sources of truth for one surface is the drift the list
// exists to end.

type capabilityEnvelope struct {
	OK        bool   `json:"ok"`
	Operation string `json:"operation"`
	Result    struct {
		Version    string   `json:"version"`
		FormatSpec string   `json:"formatSpec"`
		Operations []string `json:"operations"`
		Modifiers  []string `json:"modifiers"`
		About      string   `json:"about"`
		Usage      string   `json:"usage"`
	} `json:"result"`
	Errors []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

func capabilities(t *testing.T, args ...string) (capabilityEnvelope, result) {
	t.Helper()
	got := (runner{}).run(args...)
	var env capabilityEnvelope
	if err := json.Unmarshal([]byte(got.Stdout), &env); err != nil {
		t.Fatalf("%v is not the envelope: %v\n%s", args, err, got.Stdout)
	}
	return env, got
}

func TestHelpJSONListsWhatTheParserAccepts(t *testing.T) {
	env, got := capabilities(t, "--help", "--json")
	if got.Code != ExitOK || !env.OK || env.Operation != "help" {
		t.Fatalf("envelope = %+v (exit %d)", env, got.Code)
	}
	if env.Result.Version != Version || env.Result.FormatSpec != FormatSpecVersion {
		t.Errorf("version = %q/%q, want %q/%q",
			env.Result.Version, env.Result.FormatSpec, Version, FormatSpecVersion)
	}

	// The lists ARE the parser's tables. Comparing against the same helpers the
	// envelope is built from would prove nothing, so each name is fed back
	// through Parse: a list that promises a switch the parser rejects is the
	// failure this whole item exists to prevent.
	for _, op := range env.Result.Operations {
		if strings.HasPrefix(op, "-") {
			t.Errorf("operation %q carries a dash; §3.4.1 says bare names", op)
		}
		if _, err := Parse([]string{"--" + op}); err != nil &&
			strings.Contains(err.Error(), "unknown switch") {
			t.Errorf("--%s is listed but the parser rejects it: %v", op, err)
		}
	}
	for _, mod := range env.Result.Modifiers {
		if strings.HasPrefix(mod, "-") {
			t.Errorf("modifier %q carries a dash", mod)
		}
		// --status is a benign carrier: every invocation needs an operation,
		// and this one takes no subject to be confused with the modifier.
		if _, err := Parse([]string{"--status", "--" + mod, "x"}); err != nil &&
			strings.Contains(err.Error(), "unknown switch") {
			t.Errorf("--%s is listed but the parser rejects it: %v", mod, err)
		}
	}

	// And the other direction: a switch the parser accepts but the list omits
	// is undiscoverable, which is the same defect the help-text cross-check
	// guards against for the prose.
	listed := map[string]bool{}
	for _, n := range env.Result.Operations {
		listed[n] = true
	}
	for _, n := range env.Result.Modifiers {
		listed[n] = true
	}
	for _, name := range allSwitchNames() {
		if !listed[name] {
			t.Errorf("--%s is accepted by the parser but missing from the capability list", name)
		}
	}
}

// The operations a caller most needs to ask about are the optional ones, since
// those are the ones a build may legitimately lack.
func TestHelpJSONNamesTheOptionalOperations(t *testing.T) {
	env, _ := capabilities(t, "--help", "--json")
	has := map[string]bool{}
	for _, op := range env.Result.Operations {
		has[op] = true
	}
	for _, op := range []string{"add-many", "archive", "migrate", "stats", "tick", "search", "block"} {
		if !has[op] {
			t.Errorf("this build has --%s; the list must say so", op)
		}
	}
	// …and never names one it does not have. --describe is specified (§5.3.2)
	// and unbuilt, which is exactly the case a caller has to be able to detect.
	if has["describe"] {
		t.Error("--describe is not implemented; the list must not claim it")
	}
}

func TestHelpJSONForOneOperationCarriesItsPage(t *testing.T) {
	env, got := capabilities(t, "--help", "--add-many", "--json")
	if got.Code != ExitOK || !env.OK {
		t.Fatalf("exit %d, envelope %+v", got.Code, env)
	}
	if env.Result.About != "add-many" {
		t.Errorf("about = %q, want add-many", env.Result.About)
	}
	if !strings.Contains(env.Result.Usage, "--add-many") {
		t.Errorf("usage should be the operation's page:\n%s", env.Result.Usage)
	}
	// The whole surface still rides along, so one call answers both questions.
	if len(env.Result.Operations) == 0 {
		t.Error("the operation list should be present even when one is named")
	}
}

// An operation this build does not have is an unknown switch and fails as one,
// which is how the per-operation probe (`mm --help --OP`) works at all.
func TestHelpJSONRefusesAnOperationThisBuildLacks(t *testing.T) {
	env, got := capabilities(t, "--help", "--describe", "--json")
	if got.Code != ExitUsage {
		t.Fatalf("want exit %d, got %d", ExitUsage, got.Code)
	}
	if env.OK || len(env.Errors) == 0 {
		t.Fatalf("want a failure envelope, got %+v", env)
	}
	if !strings.Contains(env.Errors[0].Message, "describe") {
		t.Errorf("the error should name the switch: %+v", env.Errors)
	}

	// The exit-code probe, which is what a shell caller uses and what the pi
	// plugin falls back to: 0 when the build has it, 2 when it does not.
	if got := (runner{}).run("--help", "--add-many"); got.Code != ExitOK {
		t.Errorf("--help --add-many should exit 0, got %d", got.Code)
	}
	if got := (runner{}).run("--help", "--describe"); got.Code != ExitUsage {
		t.Errorf("--help --describe should exit 2, got %d", got.Code)
	}
}

// Without --json, --help is unchanged: prose for a person, on stdout, exit 0.
func TestHelpWithoutJSONIsStillProse(t *testing.T) {
	got := (runner{}).run("--help")
	if got.Code != ExitOK {
		t.Fatalf("exit %d", got.Code)
	}
	if strings.HasPrefix(strings.TrimSpace(got.Stdout), "{") {
		t.Errorf("--help alone must not emit JSON:\n%s", got.Stdout)
	}
	if !strings.Contains(got.Stdout, "Operations:") {
		t.Errorf("the human help should still list operations:\n%s", got.Stdout)
	}
	// --version keeps its line of text: a version string is already
	// machine-readable, and the capability list carries the same two values.
	ver := (runner{}).run("--version", "--json")
	if strings.HasPrefix(strings.TrimSpace(ver.Stdout), "{") {
		t.Errorf("--version --json should stay a line of text:\n%s", ver.Stdout)
	}
}
