package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// §9.2. The two requirements that matter most are structural rather than about
// any particular field: exactly one object on stdout, and the envelope present
// on failure as well as on success.

func decode(t *testing.T, got result) envelopeJSON {
	t.Helper()
	var e envelopeJSON
	dec := json.NewDecoder(strings.NewReader(got.Stdout))
	if err := dec.Decode(&e); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, got.Stdout)
	}
	// "One JSON object on stdout, nothing else" — so there must be no second
	// value after it.
	if dec.More() {
		t.Errorf("more than one value on stdout:\n%s", got.Stdout)
	}
	return e
}

// envelopeJSON mirrors the envelope for decoding, so the test reads the wire
// format rather than the struct that produced it.
type envelopeJSON struct {
	OK        bool `json:"ok"`
	Operation string
	Directory *struct {
		Path     string
		Project  string
		NextID   string `json:"nextId"`
		WipLimit int    `json:"wipLimit"`
		WipUsed  int    `json:"wipUsed"`
	}
	Result   json.RawMessage
	Changes  []struct{ Kind, ID, File, Before, After string }
	Warnings []string
	Errors   []struct{ Code, Message, ID, File string }
}

// Every operation, success and failure alike, must produce a decodable
// envelope. This is the whole contract: a caller never has to tell a JSON error
// object from crash text.
func TestJSONEnvelopeForEveryOperation(t *testing.T) {
	r, _ := v2Project(t, "--slots", "2")
	r.run("--add", "First", "--prio", "high", "--tag", "infra")
	r.run("--add", "Second")

	cases := []struct {
		name string
		args []string
		ok   bool
	}{
		{"list", []string{"--list"}, true},
		{"show", []string{"--show", "T-0001"}, true},
		{"add", []string{"--add", "Third"}, true},
		{"edit", []string{"--edit", "T-0002", "--prio", "low"}, true},
		{"move", []string{"--move", "T-0002", "--top"}, true},
		{"start", []string{"--start", "T-0002"}, true},
		{"pause", []string{"--pause", "T-0002"}, true},
		{"finish", []string{"--finish", "T-0002"}, true},
		// Legal: --remove works from done.md as well as from the backlog.
		{"remove", []string{"--remove", "T-0002", "--force"}, true},
		{"wip", []string{"--wip", "3", "--stage", "working"}, true},
		{"report", []string{"--report", "--period", "all"}, true},
		{"find", []string{"--find"}, true},
		{"check", []string{"--check"}, true},
		{"status", []string{"--status"}, true},
		{"next", []string{"--next"}, true},
		{"search", []string{"--search", "First"}, true},

		{"unknown id", []string{"--show", "T-9999"}, false},
		{"bad value", []string{"--add", "x", "--prio", "urgent"}, false},
		{"unknown switch", []string{"--list", "--nope"}, false},
		{"no operation", []string{"--verbose"}, false},
		{"guard not satisfied", []string{"--remove", "T-0001"}, false},
	}
	for _, c := range cases {
		got := r.run(append(append([]string{}, c.args...), "--json")...)
		e := decode(t, got)

		if e.OK != c.ok {
			t.Errorf("%s: ok = %v, want %v (exit %d)\n%s", c.name, e.OK, c.ok, got.Code, got.Stdout)
		}
		if c.ok && len(e.Errors) != 0 {
			t.Errorf("%s: ok but errors = %+v", c.name, e.Errors)
		}
		if !c.ok {
			if len(e.Errors) == 0 {
				t.Errorf("%s: not ok but no errors reported", c.name)
			} else if e.Errors[0].Code == "" || e.Errors[0].Message == "" {
				t.Errorf("%s: error is missing code or message: %+v", c.name, e.Errors[0])
			}
		}
		// Never mix diagnostics into the parsed stream.
		if strings.Contains(got.Stdout, "mm:") {
			t.Errorf("%s: human text leaked into the envelope:\n%s", c.name, got.Stdout)
		}
	}
}

// A parse failure must still produce an envelope — including when the failure
// is in the same command line that asked for JSON.
func TestJSONEnvelopeSurvivesAParseError(t *testing.T) {
	r := runner{cwd: t.TempDir()}

	got := r.run("--nonsense", "--json")
	if got.Code != ExitUsage {
		t.Errorf("exit = %d, want %d", got.Code, ExitUsage)
	}
	e := decode(t, got)
	if e.OK || len(e.Errors) == 0 {
		t.Fatalf("expected a failure envelope:\n%s", got.Stdout)
	}
	if e.Errors[0].Code != "InvalidArgument" {
		t.Errorf("code = %q", e.Errors[0].Code)
	}
}

func TestJSONResultShapes(t *testing.T) {
	r, _ := v2Project(t)

	// --add returns the item, with its assigned ID and its unregistered fields.
	got := r.run("--add", "Fix it", "--prio", "high", "--tag", "infra", "--tag", "ci", "--json")
	e := decode(t, got)
	var item jsonItem
	if err := json.Unmarshal(e.Result, &item); err != nil {
		t.Fatalf("result is not an item: %v", err)
	}
	if item.ID != "T-0001" || item.Title != "Fix it" || item.Prio != "high" {
		t.Errorf("item = %+v", item)
	}
	if strings.Join(item.Tags, ",") != "infra,ci" {
		t.Errorf("tags = %v", item.Tags)
	}
	if len(e.Changes) == 0 || e.Changes[0].ID != "T-0001" {
		t.Errorf("changes = %+v", e.Changes)
	}
	if e.Directory == nil || e.Directory.Project != "Test Project" {
		t.Errorf("directory = %+v", e.Directory)
	}

	// An unregistered field survives into the envelope: the JSON must not be a
	// lossy view of a format whose extension point is fields we do not know.
	r.run("--edit", "T-0001", "--set", "owner=dana")
	e = decode(t, r.run("--show", "T-0001", "--json"))
	var show struct{ Item jsonItem }
	if err := json.Unmarshal(e.Result, &show); err != nil {
		t.Fatal(err)
	}
	if show.Item.Extra["owner"] != "dana" {
		t.Errorf("extra = %v", show.Item.Extra)
	}

	// --report states its period and where it came from, in every output mode.
	e = decode(t, r.run("--report", "--period", "2026-W31", "--json"))
	var rep struct {
		Period struct{ Label, Since, Until, Source string }
	}
	if err := json.Unmarshal(e.Result, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Period.Label != "2026-W31" || rep.Period.Source != "switch" {
		t.Errorf("period = %+v", rep.Period)
	}

	// --status is the one-screen summary as data: wip, counts, slots, and the
	// featured Ready items (null when ## Ready is empty).
	e = decode(t, r.run("--status", "--json"))
	var st struct {
		Wip   struct{ Used, Limit int }
		Slots []struct {
			File     string
			Occupied bool
			Item     *jsonItem
		}
		Next *jsonItem
	}
	if err := json.Unmarshal(e.Result, &st); err != nil {
		t.Fatalf("status result: %v", err)
	}
	// Version 2 has no slots at all, and wip stays 0/0 at the top level -
	// per-stage caps are their own "stages" array, not this test's concern.
	if st.Wip.Limit != 0 || st.Wip.Used != 0 {
		t.Errorf("wip = %+v", st.Wip)
	}
	if len(st.Slots) != 0 {
		t.Errorf("slots = %+v, want none on a version-2 directory", st.Slots)
	}
	if st.Next == nil || st.Next.ID != "T-0001" {
		t.Errorf("next = %+v, want T-0001 (the top of ## Ready)", st.Next)
	}

	// --search reports each hit's field and navigable file:line.
	r.run("--add", "Deploy the script", "--stage", "ready")
	e = decode(t, r.run("--search", "deploy", "--json"))
	var hits []struct {
		Field string
		At    struct {
			File string
			Line int
		}
		Item jsonItem
	}
	if err := json.Unmarshal(e.Result, &hits); err != nil {
		t.Fatalf("search result: %v", err)
	}
	if len(hits) != 1 || hits[0].Field != "title" || hits[0].At.File != "board.md" {
		t.Errorf("hits = %+v", hits)
	}
}

// --check succeeds even when it finds problems: violations are results, not
// errors (§5.1.12). The envelope says ok:true, each directory carries its own
// verdict, and the EXIT CODE is what a script gates on.
func TestJSONCheckReportsViolationsAsResults(t *testing.T) {
	broken := fixturePath(t, "broken-i5-blocked-without-reason")
	r := runner{cwd: t.TempDir()}

	got := r.run("--dir", broken, "--check", "--json")
	if got.Code != ExitInvariantViolation {
		t.Errorf("exit = %d, want %d", got.Code, ExitInvariantViolation)
	}
	e := decode(t, got)
	if !e.OK {
		t.Errorf("--check ran successfully; ok should be true")
	}
	if len(e.Errors) != 0 {
		t.Errorf("violations are results, not errors: %+v", e.Errors)
	}

	var dirs []struct {
		Path, Project string
		OK            bool
		Violations    []struct {
			Invariant, File, Message string
			Line                     int
		}
	}
	if err := json.Unmarshal(e.Result, &dirs); err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 1 || dirs[0].OK {
		t.Fatalf("result = %+v", dirs)
	}
	v := dirs[0].Violations
	if len(v) != 1 || v[0].Invariant != "I5" || v[0].Line == 0 {
		t.Errorf("violations = %+v", v)
	}
}

// Warnings belong in the envelope, not spliced into the object stream.
func TestJSONWarnings(t *testing.T) {
	r, _ := v2Project(t)
	r.run("--add", "With a detail file", "--detail-text", "body")

	got := r.run("--remove", "T-0001", "--force", "--json")
	e := decode(t, got)
	if len(e.Warnings) == 0 {
		t.Errorf("the orphaned detail file should be warned about:\n%s", got.Stdout)
	}
	var removal struct{ DetailOrphan string }
	if err := json.Unmarshal(e.Result, &removal); err != nil {
		t.Fatal(err)
	}
	// As a field too: a caller must be able to act on the path without parsing
	// the prose in a warning.
	if !strings.HasSuffix(removal.DetailOrphan, "T-0001.md") {
		t.Errorf("detailOrphan = %q", removal.DetailOrphan)
	}
}

// F3 (code-review-007): the envelope's error id is only for operations whose
// subject IS an item id. --wip's subject is a count and --search's is a query;
// a failure there must not be labeled with the id its subject happens to parse
// as ("100" -> T-0100).
func TestJSONErrorIDOnlyForIDSubjects(t *testing.T) {
	r, _ := newProject(t)

	// A subject that is an ID keeps naming the item.
	got := r.run("--show", "T-9999", "--json")
	e := decode(t, got)
	if got.Code != ExitNotFound {
		t.Fatalf("show: exit %d, want %d", got.Code, ExitNotFound)
	}
	if len(e.Errors) != 1 || e.Errors[0].ID != "T-9999" {
		t.Errorf("show: want the error to name T-9999, got %+v", e.Errors)
	}

	// A count that parses as an ID is still a count, not an item.
	got = r.run("--wip", "100", "--json")
	e = decode(t, got)
	if got.Code != ExitUsage {
		t.Fatalf("wip: exit %d, want %d", got.Code, ExitUsage)
	}
	if len(e.Errors) != 1 || e.Errors[0].ID != "" {
		t.Errorf("wip: the error must not carry an id, got %+v", e.Errors)
	}

	// A query that parses as an ID is still a query.
	got = r.run("--search", "42", "--field", "bogus", "--json")
	e = decode(t, got)
	if got.Code != ExitUsage {
		t.Fatalf("search: exit %d, want %d", got.Code, ExitUsage)
	}
	if len(e.Errors) != 1 || e.Errors[0].ID != "" {
		t.Errorf("search: the error must not carry an id, got %+v", e.Errors)
	}
}

// §3.4: the two machine modes are mutually exclusive, and --json is no longer
// refused as unimplemented.
func TestJSONAndPorcelain(t *testing.T) {
	r, _ := newProject(t)

	if got := r.run("--list", "--json", "--porcelain"); got.Code != ExitUsage {
		t.Errorf("exit = %d, want %d", got.Code, ExitUsage)
	}
	if got := r.run("--list", "--json"); got.Code != ExitOK {
		t.Errorf("--json should work now: %s", got)
	}
}
