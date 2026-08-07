package cli

import (
	"strings"
	"testing"
)

// §9.3: line-oriented, tab-separated, stable across versions, no header.

// This test exists to make a column swap fail loudly. The compiler cannot catch
// one, and a swapped column does not break a pipeline — it silently feeds it
// the wrong data. §9.3 allows fields to be APPENDED and nothing else, so this
// list may grow at the end and may never be reordered.
func TestPorcelainFieldOrderIsFrozen(t *testing.T) {
	frozen := map[Op]string{
		OpList:    "id state section prio tags title",
		OpShow:    "id state section prio tags title",
		OpAdd:     "id state section prio tags title",
		OpAddMany: "id state section prio tags title",
		OpEdit:    "id state section prio tags title",
		OpMove:    "id state section prio tags title",
		OpStart:   "id state section prio tags title",
		OpPause:   "id state section prio tags title",
		OpFinish:  "id state section prio tags title",
		OpBlock:   "id state section prio tags title",
		OpUnblock: "id state section prio tags title",
		OpNote:    "id state section prio tags title",
		OpRemove:  "id state section prio tags title",
		OpCheck:   "path file line invariant message",
		OpFind:    "path project wipUsed wipLimit",
		OpInit:    "path project",
		OpWip:     "path project wipUsed wipLimit",
		OpReport:  "id done outcome tags title",
		OpStatus:  "wipUsed wipLimit ready blocked someday done",
		OpNext:    "id state section prio tags title",
		OpSearch:  "id state field file line text",
		OpFix:     "oldId newId file detail",
	}
	for op, want := range frozen {
		got := strings.Join(porcelainFields[op], " ")
		if got == want {
			continue
		}
		if strings.HasPrefix(got, want+" ") {
			continue // appended to, which is allowed
		}
		t.Errorf("--%s fields changed\n got: %s\nwant: %s (or that plus new fields at the end)",
			op, got, want)
	}
	// Every operation that produces records must document its columns.
	for _, op := range operations {
		if _, ok := porcelainFields[op]; !ok {
			t.Errorf("--%s has no documented porcelain fields", op)
		}
	}
}

// The output must be exactly the records: no header, no summary, no blank line.
func TestPorcelainHasNoDecoration(t *testing.T) {
	r, _ := newProject(t)
	r.run("--add", "First", "--prio", "high", "--tag", "infra", "--tag", "ci")
	r.run("--add", "Second")

	got := r.run("--list", "--porcelain")
	if got.Code != ExitOK {
		t.Fatalf("%s", got)
	}
	lines := strings.Split(strings.TrimSuffix(got.Stdout, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 records, got %d:\n%q", len(lines), got.Stdout)
	}
	for _, l := range lines {
		fields := strings.Split(l, "\t")
		if len(fields) != len(porcelainFields[OpList]) {
			t.Errorf("record has %d fields, want %d: %q",
				len(fields), len(porcelainFields[OpList]), l)
		}
	}
	first := strings.Split(lines[0], "\t")
	if first[0] != "T-0001" || first[1] != "backlog" || first[2] != "Ready" {
		t.Errorf("fields = %q", first)
	}
	if first[3] != "high" || first[4] != "infra,ci" || first[5] != "First" {
		// title is "First", the rest of the record must not have leaked into it
		t.Errorf("fields = %q", first)
	}
	// An absent prio reports its EFFECTIVE value, because a pipeline cannot
	// apply the "absent means med" rule for itself without knowing the format.
	second := strings.Split(lines[1], "\t")
	if second[3] != "med" {
		t.Errorf("prio = %q, want the effective value", second[3])
	}
}

// On failure the stream is empty: a pipeline must not be handed a partial set
// that looks complete. The reason still reaches a human, on stderr.
func TestPorcelainWritesNothingOnFailure(t *testing.T) {
	r, _ := newProject(t)

	got := r.run("--show", "T-9999", "--porcelain")
	if got.Code != ExitNotFound {
		t.Errorf("exit = %d, want %d", got.Code, ExitNotFound)
	}
	if got.Stdout != "" {
		t.Errorf("stdout should be empty:\n%q", got.Stdout)
	}
	if !strings.Contains(got.Stderr, "T-9999") {
		t.Errorf("the reason should reach stderr: %q", got.Stderr)
	}
}

// A tab or a newline inside a value would invent a column or split a record.
func TestPorcelainCannotBeBrokenByAValue(t *testing.T) {
	r, _ := newProject(t)
	r.run("--add", "Title with\ta tab and a\nnewline")

	got := r.run("--list", "--porcelain")
	lines := strings.Split(strings.TrimSuffix(got.Stdout, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("the value split the record into %d lines:\n%q", len(lines), got.Stdout)
	}
	if n := len(strings.Split(lines[0], "\t")); n != len(porcelainFields[OpList]) {
		t.Errorf("the value invented columns: %d fields", n)
	}
}

// --check reports each violation as a record, which is more than the human
// line can carry: the invariant is its own column.
func TestPorcelainCheck(t *testing.T) {
	broken := fixturePath(t, "broken-i5-blocked-without-reason")
	r := runner{cwd: t.TempDir()}

	got := r.run("--dir", broken, "--check", "--porcelain")
	if got.Code != ExitInvariantViolation {
		t.Errorf("exit = %d, want %d", got.Code, ExitInvariantViolation)
	}
	lines := strings.Split(strings.TrimSuffix(got.Stdout, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("want one record:\n%q", got.Stdout)
	}
	fields := strings.Split(lines[0], "\t")
	if len(fields) != 5 || fields[3] != "I5" {
		t.Errorf("fields = %q", fields)
	}

	// A clean directory produces no records at all, not an "ok" line.
	r2, _ := newProject(t)
	if got := r2.run("--check", "--porcelain"); got.Stdout != "" {
		t.Errorf("a clean check should emit nothing:\n%q", got.Stdout)
	}
}

func TestPorcelainAndJSONAreExclusive(t *testing.T) {
	r, _ := newProject(t)
	if got := r.run("--list", "--json", "--porcelain"); got.Code != ExitUsage {
		t.Errorf("exit = %d, want %d", got.Code, ExitUsage)
	}
}
