package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/Hopasaurus/micro-manager/mm"
)

func mustParse(t *testing.T, args ...string) *Invocation {
	t.Helper()
	in, err := Parse(args)
	if err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return in
}

func parseErr(t *testing.T, args ...string) error {
	t.Helper()
	_, err := Parse(args)
	if err == nil {
		t.Fatalf("parse %v: expected an error", args)
	}
	if !isUsage(err) {
		t.Errorf("parse %v: %v is not a usage error", args, err)
	}
	return err
}

// §3.3 rule 2: both forms, for every kind of switch.
func TestParseValueForms(t *testing.T) {
	for _, args := range [][]string{
		{"--add", "Fix it", "--prio", "high"},
		{"--add", "Fix it", "--prio=high"},
		{"--add=Fix it", "--prio=high"},
	} {
		in := mustParse(t, args...)
		if in.Op != OpAdd {
			t.Errorf("%v: op = %q", args, in.Op)
		}
		if in.Subject != "Fix it" {
			t.Errorf("%v: subject = %q", args, in.Subject)
		}
		if in.Value("prio") != "high" {
			t.Errorf("%v: prio = %q", args, in.Value("prio"))
		}
	}
}

// §3.2: exactly one operation. Zero and two are both usage errors, and the
// two-operation message names both so the user can see what collided.
func TestParseOperationCount(t *testing.T) {
	err := parseErr(t, "--add", "x", "--list")
	if !strings.Contains(err.Error(), "add") || !strings.Contains(err.Error(), "list") {
		t.Errorf("the error should name both operations: %v", err)
	}
	parseErr(t)
	parseErr(t, "--verbose", "--quiet")

	// --help and --version answer with no operation at all.
	if in := mustParse(t, "--help"); !in.Help {
		t.Error("--help should parse alone")
	}
	if in := mustParse(t, "--version"); !in.Version {
		t.Error("--version should parse alone")
	}
	// --help with an operation asks for that operation's usage.
	in := mustParse(t, "--help", "--start")
	if !in.Help || in.Op != OpStart {
		t.Errorf("help = %v, op = %q", in.Help, in.Op)
	}
}

// §3.3 rule 6: an unknown switch is an error, never silently ignored.
func TestParseUnknownSwitch(t *testing.T) {
	err := parseErr(t, "--list", "--tagz", "infra")
	if !strings.Contains(err.Error(), "tagz") {
		t.Errorf("the error should name the switch: %v", err)
	}
	// A near miss gets a suggestion, because the switch namespace is long.
	if !strings.Contains(err.Error(), "--tag?") {
		t.Errorf("expected a suggestion: %v", err)
	}
	// Something unrecognisable gets no misleading guess.
	err = parseErr(t, "--list", "--xyzzy")
	if strings.Contains(err.Error(), "did you mean") {
		t.Errorf("no suggestion should be offered for %v", err)
	}
}

// §3.3 rule 5: last wins, except where documented as accumulating.
func TestParseRepeatedSwitches(t *testing.T) {
	in := mustParse(t, "--add", "x", "--prio", "high", "--prio", "low")
	if in.Value("prio") != "low" {
		t.Errorf("prio = %q, want the last one", in.Value("prio"))
	}

	in = mustParse(t, "--add", "x", "--tag", "infra", "--tag", "ci", "--tag", "infra")
	if strings.Join(in.Tags, ",") != "infra,ci,infra" {
		t.Errorf("tags = %v; --tag accumulates and does not deduplicate here", in.Tags)
	}
	in = mustParse(t, "--edit", "1", "--set", "a=1", "--set", "b=2", "--unset", "c")
	if len(in.Sets) != 2 || len(in.Unsets) != 1 {
		t.Errorf("sets = %v, unsets = %v", in.Sets, in.Unsets)
	}
}

// §3.3 rule 3.
func TestParseDoubleDashEndsSwitches(t *testing.T) {
	in := mustParse(t, "--add", "--", "--not-a-switch", "--neither")
	if in.Op != OpAdd {
		t.Fatalf("op = %q", in.Op)
	}
	if strings.Join(in.Rest, " ") != "--not-a-switch --neither" {
		t.Errorf("rest = %v", in.Rest)
	}
}

// §3.3 rule 4: the bare number is accepted, because typing the prefix is
// friction the format imposes for machine reasons. Matching is otherwise exact:
// a case deviation is rejected, not repaired (spec-file-format.md §3.3.2 rule 2).
func TestParseIDForms(t *testing.T) {
	g := mm.DefaultIDGrammar()
	for _, s := range []string{"T-0042", "42", "0042"} {
		id, err := parseIDIn(s, g)
		if err != nil {
			t.Errorf("%q: %v", s, err)
			continue
		}
		if id != "T-0042" {
			t.Errorf("%q -> %q, want T-0042", s, id)
		}
	}
	for _, s := range []string{"", "abc", "T-", "t-0042", "T-42", "99999", "T-00042"} {
		if _, err := parseIDIn(s, g); err == nil {
			t.Errorf("%q should not parse as an id", s)
		}
	}
}

// TestParseIDInDeclaredGrammar is the CLI half of the loosen-and-validate
// contract (spec-tools.md §3.3 rule 4, spec-file-format.md §3.3.2): the CLI
// accepts the directory's own form plus the bare number, and anything from
// another grammar is a usage error, not a repair.
func TestParseIDInDeclaredGrammar(t *testing.T) {
	g := mm.IDGrammar{Prefix: "X", Width: 3}
	for s, want := range map[string]mm.ID{
		"X-001": "X-001",
		"1":     "X-001",
		"001":   "X-001",
		"X-042": "X-042",
		"42":    "X-042",
	} {
		id, err := parseIDIn(s, g)
		if err != nil {
			t.Errorf("%q: %v", s, err)
			continue
		}
		if id != want {
			t.Errorf("%q -> %q, want %q", s, id, want)
		}
	}
	for _, s := range []string{"T-001", "x-001", "X-1", "X-0001", "M-001", "9999"} {
		if _, err := parseIDIn(s, g); err == nil {
			t.Errorf("%q should not parse against %s", s, g)
		}
	}
}

func TestParseMissingValue(t *testing.T) {
	err := parseErr(t, "--add", "x", "--prio")
	if !strings.Contains(err.Error(), "needs a value") {
		t.Errorf("%v", err)
	}
	// A switch following one that wants a value is a mistake, not a value.
	err = parseErr(t, "--add", "x", "--prio", "--verbose")
	if !strings.Contains(err.Error(), "needs a value") {
		t.Errorf("%v", err)
	}
	// -- ends switch parsing and is never a value either: --prio -- must not
	// set prio to "--" (code-review-007 F8).
	err = parseErr(t, "--add", "x", "--prio", "--")
	if !strings.Contains(err.Error(), "needs a value") {
		t.Errorf("%v", err)
	}
	// But a negative number is a value, so --position -1 stays possible.
	in := mustParse(t, "--move", "1", "--position", "-1")
	if in.Value("position") != "-1" {
		t.Errorf("position = %q", in.Value("position"))
	}
}

func TestParseGlobalConflicts(t *testing.T) {
	parseErr(t, "--list", "--json", "--porcelain")
	parseErr(t, "--check", "--all", "--dir", "somewhere")

	// The remove guard is a rule about the invocation, not about the library:
	// --yes must not satisfy --force (§5.1.6).
	err := parseErr(t, "--remove", "T-0001", "--yes")
	if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("the refusal should point at the alternative: %v", err)
	}
	if _, err := Parse([]string{"--remove", "T-0001", "--force"}); err != nil {
		t.Errorf("--force alone should be accepted: %v", err)
	}
}

// A boolean accepts an explicit value so a script can turn a default off.
func TestParseBooleanValues(t *testing.T) {
	in := mustParse(t, "--list", "--verbose=false")
	if in.Verbose {
		t.Error("--verbose=false should be false")
	}
	in = mustParse(t, "--list", "--verbose")
	if !in.Verbose {
		t.Error("--verbose should be true")
	}
	parseErr(t, "--list", "--verbose=maybe")
}

func TestUsageErrorIsTyped(t *testing.T) {
	_, err := Parse([]string{"--nonsense"})
	var ue *UsageError
	if !errors.As(err, &ue) {
		t.Fatalf("want a *UsageError, got %T", err)
	}
	if exitCode(err) != ExitUsage {
		t.Errorf("exit code = %d, want %d", exitCode(err), ExitUsage)
	}
}

// The help text and the parser must not drift: a switch the parser accepts and
// the help never mentions is undiscoverable, and one the help promises and the
// parser rejects is a lie.
func TestHelpCoversEverySwitch(t *testing.T) {
	all := generalUsage
	for _, text := range operationUsage {
		all += text
	}
	for _, name := range allSwitchNames() {
		if !strings.Contains(all, "--"+name) {
			t.Errorf("--%s is accepted by the parser but appears in no help text", name)
		}
	}
	// And every operation has its own page.
	for _, op := range operations {
		if _, ok := operationUsage[op]; !ok {
			t.Errorf("--%s has no per-operation help", op)
		}
	}
}
