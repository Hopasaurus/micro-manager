package mm

import (
	"fmt"
	"strings"
)

// Bulk add (spec-tools.md §5.2.1).
//
// AddMany is Add in a loop with ONE difference, and the difference is the whole
// point: there is one transaction. All the items are built, all the IDs are
// allocated, the result is validated once, and backlog.md is written once. A
// bad line means nothing is written at all.
//
// A caller looping over Add gets the opposite — eleven items added and the
// twelfth rejected, with no record of where the run stopped — which is why this
// is not left to the front end.

// ParseAddLine parses one line of --add-many input into an AddRequest
// (spec-tools.md §5.2.1).
//
// The grammar is the format's item line (spec-file-format.md §4.2) with the box
// and the ID removed, because the ID is allocated by the operation and never
// supplied:
//
//	TITLE
//	TITLE | prio:high | tags:infra,ci
//	- TITLE | prio:low
//
// It lives in the library rather than in the CLI because a GUI with a paste box
// needs the same grammar (spec-tools.md §2.5), and two implementations of a
// grammar drift. What the library will NOT do is find the lines: reading a file
// or stdin belongs to the host (§2.2 rule 4).
//
// lineNo is used only to locate errors; pass the 1-based line number of the
// input, or 0 when there is nothing to locate.
func ParseAddLine(lineNo int, line string) (AddRequest, error) {
	var req AddRequest

	text := strings.TrimSpace(stripBullet(line))
	if text == "" {
		return req, addLineErrf(lineNo, "the line is empty")
	}
	// A pasted item line brings an ID with it, and honouring that ID would
	// hand out a number that is retired or already in use (I2). Refusing and
	// naming the fix beats silently renumbering what someone pasted.
	if id, ok := leadingBracket(text); ok {
		return req, addLineErrf(lineNo,
			"%q looks like an existing item line: --add-many allocates ids, so remove the [%s]",
			truncateLine(text), id)
	}

	// A line that opens with a field has lost its title somewhere — usually a
	// copied line whose title was trimmed off. Diagnosing that before the pipe
	// rule below says what is actually wrong with it.
	if strings.HasPrefix(text, "|") {
		return req, addLineErrf(lineNo, "the line has no title, only fields")
	}

	parts := strings.Split(text, fieldSep)
	req.Title = strings.TrimSpace(parts[0])
	if req.Title == "" {
		return req, addLineErrf(lineNo, "the line has no title")
	}
	if strings.Contains(req.Title, "|") {
		// A pipe that did not become a separator: " |x" or "a| b". Saying which
		// half of the rule was missed is more useful than "malformed line".
		return req, addLineErrf(lineNo,
			"a title may not contain %q; fields are separated by %q", "|", fieldSep)
	}

	seen := map[string]bool{}
	for _, part := range parts[1:] {
		key, val, ok := strings.Cut(part, ":")
		if !ok {
			return req, addLineErrf(lineNo, "field %q is not key:value", strings.TrimSpace(part))
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		if seen[key] {
			return req, addLineErrf(lineNo, "the line repeats field %s", key)
		}
		seen[key] = true
		if err := applyAddField(&req, lineNo, key, val); err != nil {
			return req, err
		}
	}
	return req, nil
}

// applyAddField sets one key:value pair on a request.
//
// The accepted keys are EXACTLY the ones --add can set (§5.1.2). A registered
// field that belongs to a later state — done:, outcome:, started:, tickled: —
// is refused rather than dropped, because an item created with one would not be
// a backlog item at all; and detail: is refused because its path must match an
// ID that does not exist until this operation allocates one (I8).
func applyAddField(req *AddRequest, lineNo int, key, val string) error {
	switch key {
	case "prio":
		p, err := ParsePrio(val)
		if err != nil {
			return addLineErrf(lineNo, "prio:%s is not high, med or low", val)
		}
		req.Prio = p
	case "tags":
		tags, err := ParseTags(val)
		if err != nil {
			return addLineErrf(lineNo, "tags:%s is not a comma-separated tag list", val)
		}
		req.Tags = tags
	case "refs":
		refs, err := ParseRefs(val)
		if err != nil {
			return addLineErrf(lineNo, "refs:%s is not a reference list", val)
		}
		req.Refs = refs
	case "created":
		d, err := ParseDate(val)
		if err != nil {
			return addLineErrf(lineNo, "created:%s is not a date (want YYYY-MM-DD)", val)
		}
		req.Created = d
	case "blocked":
		// Exactly as --add reads it: a reason implies the Blocked section, so
		// one run may write into two sections.
		req.Blocked = val
		if req.Section == "" {
			req.Section = SectionBlocked
		}
	case "tickler":
		if _, err := ParseSchedule(val); err != nil {
			return addLineErrf(lineNo, "tickler:%s is not a schedule (%v)", val, err)
		}
		req.Tickler = val
	case "detail":
		return addLineErrf(lineNo,
			"detail: cannot be set here: the path has to match the item's own id (I8), "+
				"and the id is allocated by this operation. Add the items, then write the detail files")
	case "started", "done", "outcome", "tickled":
		return addLineErrf(lineNo,
			"%s: belongs to an item that has been started or closed; --add-many creates backlog items", key)
	default:
		if key == "" {
			return addLineErrf(lineNo, "a field has no key")
		}
		// Unregistered, and preserved verbatim: the format's extension point
		// (spec-file-format.md §9) does not stop at the boundary of a bulk add.
		req.Extra = append(req.Extra, Field{Key: key, Value: val})
	}
	return nil
}

// stripBullet removes a leading markdown bullet, so a checklist pasted out of a
// document is valid input as it stands.
//
// "- [ ] " is stripped before "- " so the box does not survive into the title.
// A checked box is stripped too: what someone pasted is what they want on the
// board, and an item added as done is not a thing this operation can produce.
func stripBullet(line string) string {
	s := strings.TrimLeft(line, " \t")
	for _, prefix := range []string{"- [ ] ", "- [x] ", "- [X] ", "* [ ] ", "- ", "* "} {
		if rest, ok := strings.CutPrefix(s, prefix); ok {
			return rest
		}
	}
	return s
}

// leadingBracket returns the contents of a leading "[…]" token when it looks
// like an item id — which is what a pasted item line has left once the box has
// been stripped.
//
// The shape is checked, not just the brackets: "[WIP] Fix the deploy script" is
// a perfectly good title, and refusing it would make a whole class of title
// unaddable to punish a formatting habit. What is refused is `[LETTERS-DIGITS]`,
// which is an id in ANY directory's grammar (spec-file-format.md §3.3.2) —
// deliberately not this directory's grammar alone, because a board with its own
// prefix would otherwise silently accept another board's ids as titles.
func leadingBracket(text string) (string, bool) {
	if !strings.HasPrefix(text, "[") {
		return "", false
	}
	end := strings.Index(text, "]")
	if end < 2 || end == len(text)-1 {
		return "", false
	}
	inner := text[1:end]
	prefix, digits, ok := strings.Cut(inner, "-")
	if !ok || prefix == "" || len(prefix) > 4 || digits == "" {
		return "", false
	}
	for _, r := range prefix {
		if r < 'A' || r > 'Z' {
			return "", false
		}
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return inner, true
}

func addLineErrf(lineNo int, format string, args ...any) error {
	if lineNo > 0 {
		return fmt.Errorf("%w: line %d: %s", ErrInvalidArgument, lineNo, fmt.Sprintf(format, args...))
	}
	return fmt.Errorf("%w: %s", ErrInvalidArgument, fmt.Sprintf(format, args...))
}

func truncateLine(s string) string {
	const limit = 60
	if len(s) <= limit {
		return s
	}
	return s[:limit-1] + "…"
}

// AddMany creates several backlog items in one transaction (spec-tools.md
// §5.2.1).
//
// Order is the caller's order. With Top set, the batch goes to the top of its
// section still in that order — a --top that reversed the batch would be a
// surprise nobody wants, since a list someone pasted is a list they had already
// put in an order.
//
// Each request carries its own fields; a front end applies run-wide defaults
// before calling, because "the modifier is a default and the line wins" is a
// rule about a command line, not about the library.
func (s *Store) AddMany(reqs []AddRequest, today Date) ([]Item, TxResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(reqs) == 0 {
		return nil, TxResult{}, fmt.Errorf("%w: nothing to add", ErrInvalidArgument)
	}

	t, err := s.begin()
	if err != nil {
		return nil, TxResult{}, err
	}
	b, e, err := t.backlog()
	if err != nil {
		return nil, TxResult{}, err
	}
	g := t.model.grammar()

	// Where the next item of the batch goes when Top is set. One counter per
	// section, because a line's blocked: reason can send it somewhere else and
	// each section's run has to stay in the input's order.
	topAt := map[Section]int{}

	dryRun := false
	// Pointers, not copies: InsertItem tracks each item's line and later
	// inserts shift the ones above them, so a snapshot taken inside the loop
	// would report a line number that stopped being true.
	made := make([]*Item, 0, len(reqs))
	for i, req := range reqs {
		if req.DryRun {
			// A batch is one transaction, so one request cannot be a dry run
			// while another is not: any of them asking makes the whole run one.
			dryRun = true
		}
		if req.DetailBody != "" {
			return nil, TxResult{}, fmt.Errorf(
				"%w: item %d: --add-many does not write detail files; add the items, then write them",
				ErrInvalidArgument, i+1)
		}

		it, err := buildNewItem(req, today)
		if err != nil {
			// The index is the caller's handle on which request failed; the
			// CLI turns it back into an input line number.
			return nil, TxResult{}, fmt.Errorf("item %d: %w", i+1, err)
		}
		next, err := allocNext(e, b, g)
		if err != nil {
			return nil, TxResult{}, fmt.Errorf("item %d: %w", i+1, err)
		}
		it.ID = next

		sec := it.Section
		index := len(sectionItems(b, sec)) // bottom by default
		if req.Top {
			index = topAt[sec]
			topAt[sec]++
		}
		b.InsertItem(e, sec, index, it)
		t.record(Change{Kind: ChangeCreated, ID: it.ID, File: "backlog.md",
			After: RenderItemLine(it)})
		made = append(made, it)
	}

	touchUpdated(e, today)
	t.stage("backlog.md")
	res, err := t.commit(dryRun)
	if err != nil {
		return nil, res, err
	}
	out := make([]Item, 0, len(made))
	for _, it := range made {
		out = append(out, *it)
	}
	return out, res, nil
}
