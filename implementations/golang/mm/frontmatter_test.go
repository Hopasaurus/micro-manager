package mm

import (
	"errors"
	"strings"
	"testing"
)

func parseFM(t *testing.T, s string) *Frontmatter {
	t.Helper()
	fm, err := parseFrontmatter("test.md", strings.Split(s, "\n"))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return fm
}

func TestFrontmatterBasics(t *testing.T) {
	fm := parseFM(t, `---
doc: backlog
version: 1
project: micro-manager — Go implementation
next_id: T-0046
updated: 2026-07-29
---

# Backlog
`)
	want := map[string]string{
		"doc": "backlog", "version": "1",
		"project": "micro-manager — Go implementation",
		"next_id": "T-0046", "updated": "2026-07-29",
	}
	for k, v := range want {
		if got := fm.Get(k); got != v {
			t.Errorf("Get(%q) = %q, want %q", k, got, v)
		}
	}
	if fm.End != 7 {
		t.Errorf("End = %d, want 7", fm.End)
	}
	if fm.Line("next_id") != 5 {
		t.Errorf("Line(next_id) = %d, want 5", fm.Line("next_id"))
	}
	keys := fm.Keys()
	if strings.Join(keys, ",") != "doc,version,project,next_id,updated" {
		t.Errorf("key order not preserved: %v", keys)
	}
}

func TestFrontmatterMustOpenAndClose(t *testing.T) {
	_, err := parseFrontmatter("x.md", strings.Split("# Done\n\nno frontmatter\n", "\n"))
	if err == nil || !strings.Contains(err.Error(), "must open with YAML frontmatter") {
		t.Errorf("missing opening --- should be an error, got %v", err)
	}
	_, err = parseFrontmatter("x.md", strings.Split("---\ndoc: backlog\n\n# Backlog\n", "\n"))
	if err == nil || !strings.Contains(err.Error(), "not terminated") {
		t.Errorf("unterminated block should be an error, got %v", err)
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("parse errors should unwrap to ErrInvalidArgument, got %v", err)
	}
}

// Rule 1: split at the FIRST colon. Titles routinely contain colons.
func TestFrontmatterSplitsAtFirstColon(t *testing.T) {
	fm := parseFM(t, "---\ntitle: Define the core types: Item, Slot, Directory\n---\n")
	if got := fm.Get("title"); got != "Define the core types: Item, Slot, Directory" {
		t.Errorf("got %q", got)
	}
}

// Rule 5, and the reason for the title exemption.
func TestFrontmatterCommentStripping(t *testing.T) {
	fm := parseFM(t, `---
detail: details/T-0042.md   # or: null
status: working  # in progress
title: Fix issue #42
weird: value#nospace
---
`)
	if got := fm.Get("detail"); got != "details/T-0042.md" {
		t.Errorf("comment not stripped: %q", got)
	}
	if got := fm.Get("status"); got != "working" {
		t.Errorf("comment not stripped: %q", got)
	}
	// The whole point of the exemption: this must survive intact.
	if got := fm.Get("title"); got != "Fix issue #42" {
		t.Errorf("title must be exempt from comment stripping, got %q", got)
	}
	// A '#' not preceded by whitespace is part of the value.
	if got := fm.Get("weird"); got != "value#nospace" {
		t.Errorf("got %q", got)
	}
}

// Rule 4: exactly one layer of double quotes, no other unescaping.
func TestFrontmatterQuoteStripping(t *testing.T) {
	fm := parseFM(t, `---
a: "quoted"
b: "unbalanced
c: 'single'
d: ""
e: "with \"inner\" quotes"
---
`)
	cases := map[string]string{
		"a": "quoted",
		"b": `"unbalanced`,
		"c": "'single'", // single quotes are not stripped
		"d": "",
		"e": `with \"inner\" quotes`, // no unescaping
	}
	for k, want := range cases {
		if got := fm.Get(k); got != want {
			t.Errorf("Get(%q) = %q, want %q", k, got, want)
		}
	}
}

// Rules 2 and 3.
func TestFrontmatterTrimmingAndSkipping(t *testing.T) {
	fm := parseFM(t, "---\n   spaced   :    value   \na line with no colon\n\tkey\t:\tv\t\n---\n")
	if got := fm.Get("spaced"); got != "value" {
		t.Errorf("got %q", got)
	}
	if got := fm.Get("key"); got != "v" {
		t.Errorf("tabs should be trimmed, got %q", got)
	}
	if len(fm.Keys()) != 2 {
		t.Errorf("colonless line should be ignored, keys = %v", fm.Keys())
	}
}

// Rule 6: last wins, and the key keeps its original position.
func TestFrontmatterDuplicateKeys(t *testing.T) {
	fm := parseFM(t, "---\na: 1\nb: 2\na: 3\n---\n")
	if fm.Get("a") != "3" {
		t.Errorf("last should win, got %q", fm.Get("a"))
	}
	if got := strings.Join(fm.Keys(), ","); got != "a,b" {
		t.Errorf("duplicate should not reorder or repeat: %q", got)
	}
}

func TestFrontmatterIsNull(t *testing.T) {
	fm := parseFM(t, "---\na: null\nb:\nc: value\n---\n")
	for _, k := range []string{"a", "b", "missing"} {
		if !fm.IsNull(k) {
			t.Errorf("%q should be null", k)
		}
	}
	if fm.IsNull("c") {
		t.Error("c should not be null")
	}
	// Has distinguishes absent from empty; IsNull deliberately does not.
	if !fm.Has("b") || fm.Has("missing") {
		t.Error("Has is wrong")
	}
}

func TestFrontmatterSetDeleteRender(t *testing.T) {
	fm := parseFM(t, "---\ndoc: working\nstatus: idle\nid: null\n---\n")
	fm.Set("status", "working")
	fm.Set("id", "T-0042")
	fm.Set("started", "2026-07-29")
	fm.Delete("doc")

	want := "---\nstatus: working\nid: T-0042\nstarted: 2026-07-29\n---\n"
	if got := fm.Render(); got != want {
		t.Errorf("Render() =\n%q\nwant\n%q", got, want)
	}
	fm.Delete("nonexistent") // must not panic or alter anything
	if fm.Render() != want {
		t.Error("deleting an absent key changed the block")
	}
}

// The property T-0008 depends on: an unmodified block renders back unchanged.
func TestFrontmatterRoundTrip(t *testing.T) {
	src := "---\ndoc: working\nversion: 1\nstatus: idle\nid: null\ntags: null\n---\n"
	fm := parseFM(t, src)
	if got := fm.Render(); got != src {
		t.Errorf("round trip changed the block:\ngot  %q\nwant %q", got, src)
	}
}

// Not YAML: a value that looks like a collection is an opaque string. This is
// why tags uses a comma-separated TAGLIST in both places instead.
func TestFrontmatterDoesNotParseCollections(t *testing.T) {
	fm := parseFM(t, "---\ntags: [infra, ci]\n---\n")
	if got := fm.Get("tags"); got != "[infra, ci]" {
		t.Errorf("collection should stay an opaque string, got %q", got)
	}
	if _, err := ParseTags(fm.Get("tags")); err == nil {
		t.Error("the flow-sequence form should fail TAGLIST validation")
	}
}
