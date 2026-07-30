package cli

import (
	"errors"
	"strings"
	"testing"
)

// §3.5: VISUAL wins where both are set.
func TestResolveEditor(t *testing.T) {
	cases := []struct {
		visual, editor string
		want           string
	}{
		{"vim", "ed", "vim"},               // VISUAL wins
		{"", "ed", "ed"},                   // EDITOR when VISUAL is unset
		{"   ", "ed", "ed"},                // and when it is only whitespace
		{"", "", ""},                       // neither is not an error
		{"code --wait", "", "code --wait"}, // arguments are kept
	}
	for _, c := range cases {
		got := strings.Join(resolveEditor(c.visual, c.editor), " ")
		if got != c.want {
			t.Errorf("VISUAL=%q EDITOR=%q -> %q, want %q", c.visual, c.editor, got, c.want)
		}
	}
}

// The value is split into a program and arguments, NOT handed to a shell: a
// semicolon in the variable must not be able to run a second command.
func TestEditorIsNotRunThroughAShell(t *testing.T) {
	var gotProgram string
	var gotArgs []string
	env := Env{
		Editor: "myeditor --wait; rm -rf /",
		Launch: func(program string, args []string) error {
			gotProgram, gotArgs = program, args
			return nil
		},
	}
	if err := openEditor(env, "/tmp/x/details/T-0001.md"); err != nil {
		t.Fatal(err)
	}
	if gotProgram != "myeditor" {
		t.Errorf("program = %q", gotProgram)
	}
	// The dangerous text is inert: it arrives as ordinary arguments to a
	// program that is unlikely to exist, rather than as shell syntax.
	want := "--wait; rm -rf / /tmp/x/details/T-0001.md"
	if strings.Join(gotArgs, " ") != want {
		t.Errorf("args = %q, want %q", strings.Join(gotArgs, " "), want)
	}
}

// Every case where handing over the terminal would be wrong rather than merely
// unwanted.
func TestWantsEditor(t *testing.T) {
	env := Env{Editor: "vim"}

	cases := []struct {
		name        string
		args        []string
		interactive bool
		want        bool
	}{
		{"a terminal and an editor", []string{"--add", "x", "--detail"}, true, true},
		{"--no-edit", []string{"--add", "x", "--detail", "--no-edit"}, true, false},
		{"--quiet", []string{"--add", "x", "--detail", "--quiet"}, true, false},
		{"--json", []string{"--add", "x", "--detail", "--json"}, true, false},
		{"--porcelain", []string{"--add", "x", "--detail", "--porcelain"}, true, false},
		{"--dry-run", []string{"--add", "x", "--detail", "--dry-run"}, true, false},
		{"no terminal", []string{"--add", "x", "--detail"}, false, false},
	}
	for _, c := range cases {
		in, err := Parse(c.args)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := wantsEditor(in, env, c.interactive); got != c.want {
			t.Errorf("%s: wantsEditor = %v, want %v", c.name, got, c.want)
		}
	}

	// With no editor configured there is nothing to launch, which is not an
	// error — it is the common case on a machine that has never set one.
	in, _ := Parse([]string{"--add", "x", "--detail"})
	if wantsEditor(in, Env{}, true) {
		t.Error("no EDITOR and no VISUAL should mean no editor")
	}
}

// --add --detail opens the file it just created, and passes the real path.
func TestAddDetailOpensTheEditor(t *testing.T) {
	r, dir := newProject(t)

	var opened string
	env := Env{
		Editor:      "vim",
		Interactive: true,
		Launch: func(program string, args []string) error {
			opened = args[len(args)-1]
			return nil
		},
	}
	got := r.runWith(env, "--add", "Needs a description", "--detail")
	if got.Code != ExitOK {
		t.Fatalf("%s", got)
	}
	if !strings.HasSuffix(opened, "details/T-0001.md") {
		t.Errorf("opened %q", opened)
	}
	if !strings.HasPrefix(opened, dir) {
		t.Errorf("opened %q, which is not inside %q", opened, dir)
	}

	// No detail file, nothing to open.
	opened = ""
	if got := r.runWith(env, "--add", "Just a line"); got.Code != ExitOK {
		t.Fatal(got)
	}
	if opened != "" {
		t.Errorf("opened %q for an item with no detail file", opened)
	}
}

// A failed launch must not fail the operation: the item and its detail file are
// already written and validated, and losing that over a misconfigured $EDITOR
// would be the tool destroying good work over a preference.
func TestEditorFailureDoesNotFailTheOperation(t *testing.T) {
	r, dir := newProject(t)

	env := Env{
		Editor:      "definitely-not-a-real-editor",
		Interactive: true,
		Launch: func(string, []string) error {
			return errors.New("exec: not found")
		},
	}
	got := r.runWith(env, "--add", "Still recorded", "--detail")
	if got.Code != ExitOK {
		t.Errorf("exit = %d, want %d: the item was written", got.Code, ExitOK)
	}
	if !strings.Contains(got.Stderr, "could not open an editor") {
		t.Errorf("the failure should be reported: %q", got.Stderr)
	}
	// And the work survived.
	if !strings.Contains(readFileAt(t, dir, "backlog.md"), "T-0001") {
		t.Error("the item was lost")
	}
	if !strings.Contains(readFileAt(t, dir, "details/T-0001.md"), "T-0001") {
		t.Error("the detail file was lost")
	}
}
