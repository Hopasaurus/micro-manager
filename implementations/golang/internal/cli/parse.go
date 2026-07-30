package cli

import (
	"fmt"
	"sort"
	"strings"

	"micromanager/mm"
)

// The switch parser (spec-tools.md §3).
//
// Hand-rolled, and the reason is in the shape of the surface rather than in any
// dislike of the standard library. `flag` cannot express the three things this
// specification is built on: repeated accumulating switches (--tag infra --tag
// ci), "exactly one of this set" across a namespace of operations, and a switch
// that is valid with or without a value. Every third-party parser that does
// handle those is built around POSITIONAL SUBCOMMANDS, which §3.2 explicitly
// rejects. Adopting one would mean fighting its model to express `mm --add`
// rather than `mm add`, and taking on a dependency to do it.
//
// The surface is flat by design, and flat is the case a hand-rolled parser
// handles well.

// Op is an operation switch: exactly one per invocation.
type Op string

const (
	OpNone   Op = ""
	OpInit   Op = "init"
	OpAdd    Op = "add"
	OpList   Op = "list"
	OpShow   Op = "show"
	OpEdit   Op = "edit"
	OpRemove Op = "remove"
	OpMove   Op = "move"
	OpStart  Op = "start"
	OpPause  Op = "pause"
	OpFinish Op = "finish"
	OpReport Op = "report"
	OpCheck  Op = "check"
	OpWip    Op = "wip"
	OpFind   Op = "find"
)

// takesValue reports whether an operation switch consumes the argument after it
// as its subject. §3.2: each operation takes its primary subject as its value
// where one exists.
var opTakesValue = map[Op]bool{
	OpShow: true, OpEdit: true, OpRemove: true, OpMove: true,
	OpStart: true, OpPause: true, OpFinish: true, OpAdd: true,
	OpWip: true,
}

// operations maps the switch name to its operation. Operation and modifier
// switches share one namespace (§3.2), so nothing here may be reused below.
var operations = map[string]Op{
	"init": OpInit, "add": OpAdd, "list": OpList, "show": OpShow,
	"edit": OpEdit, "remove": OpRemove, "move": OpMove, "start": OpStart,
	"pause": OpPause, "finish": OpFinish, "report": OpReport, "check": OpCheck,
	"wip": OpWip, "find": OpFind,
}

// Invocation is one parsed command line.
//
// Values are kept as strings and converted by the operation that uses them, so
// that a bad value is reported against the switch the user actually typed.
type Invocation struct {
	Op      Op
	Subject string // the operation switch's own value

	// Global modifiers (§3.4).
	Dir       string
	JSON      bool
	Porcelain bool
	DryRun    bool
	Yes       bool
	Force     bool
	Quiet     bool
	Verbose   bool
	Help      bool
	Version   bool
	All       bool

	// Operation modifiers. Presence matters for several of them, so the ones
	// that can be legitimately empty are tracked in Seen.
	Values map[string]string
	Seen   map[string]bool

	// Accumulating switches (§3.3 rule 5).
	Tags     []string
	Untags   []string
	Sets     []string
	Unsets   []string
	Rest     []string // positional values after --
	Booleans map[string]bool
}

// Has reports whether a modifier was given at all, which is not the same as its
// value being non-empty: `--blocked ""` is a different request from no --blocked.
func (in *Invocation) Has(name string) bool { return in.Seen[name] }

// Value returns a modifier's value, or "".
func (in *Invocation) Value(name string) string { return in.Values[name] }

// Bool reports whether a boolean modifier was given.
func (in *Invocation) Bool(name string) bool { return in.Booleans[name] }

// modifiers that take a value.
var valueModifiers = map[string]bool{
	"dir": true, "position": true, "before": true, "after": true,
	"section": true, "prio": true, "title": true, "blocked": true,
	"created": true, "started": true, "done": true, "outcome": true,
	"detail-text": true, "detail-file": true, "slot": true,
	"project": true, "slots": true, "slot-width": true,
	"period": true, "week": true, "since": true, "until": true,
	"group-by": true, "state": true, "limit": true, "note": true,
}

// modifiers that accumulate rather than replace.
var accumulating = map[string]bool{
	"tag": true, "untag": true, "set": true, "unset": true,
}

// boolean modifiers.
var boolModifiers = map[string]bool{
	"json": true, "porcelain": true, "dry-run": true, "yes": true,
	"force": true, "quiet": true, "verbose": true, "help": true,
	"version": true, "all": true, "top": true, "end": true,
	"detail": true, "with-detail": true,
	"last-week": true, "this-week": true, "include-wip": true,
	"include-backlog": true, "include-archives": true, "keep-notes": true,
	"discard-notes": true, "blocked-only": true,
}

// UsageError is a §3 parsing failure. It maps to exit code 2.
type UsageError struct{ Message string }

func (e *UsageError) Error() string { return e.Message }

func usagef(format string, args ...any) error {
	return &UsageError{Message: fmt.Sprintf(format, args...)}
}

// Parse turns argv into an Invocation.
func Parse(args []string) (*Invocation, error) {
	in := &Invocation{
		Values:   map[string]string{},
		Seen:     map[string]bool{},
		Booleans: map[string]bool{},
	}

	i := 0
	for ; i < len(args); i++ {
		arg := args[i]

		// §3.3 rule 3: -- ends switch parsing.
		if arg == "--" {
			in.Rest = append(in.Rest, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") {
			in.Rest = append(in.Rest, arg)
			continue
		}

		name, inlineValue, hasInline := splitSwitch(arg)
		if name == "" {
			return nil, usagef("%q is not a switch", arg)
		}

		// An operation switch. Exactly one per invocation; a second is an error
		// naming both, because guessing which the user meant is worse than
		// refusing (§3.2).
		if op, isOp := operations[name]; isOp {
			if in.Op != OpNone {
				return nil, usagef(
					"--%s and --%s are both operations; give exactly one", in.Op, op)
			}
			in.Op = op
			switch {
			case hasInline:
				in.Subject = inlineValue
			case opTakesValue[op] && i+1 < len(args) && args[i+1] != "--" && !isSwitch(args[i+1]):
				i++
				in.Subject = args[i]
			}
			continue
		}

		switch {
		case boolModifiers[name]:
			// A boolean accepts --flag=false so a script can override a default
			// without special-casing the switch away.
			value := true
			if hasInline {
				switch strings.ToLower(inlineValue) {
				case "true", "yes", "1":
				case "false", "no", "0":
					value = false
				default:
					return nil, usagef("--%s takes true or false, got %q", name, inlineValue)
				}
			}
			in.Booleans[name] = value
			in.Seen[name] = true
			applyGlobalBool(in, name, value)

		case accumulating[name]:
			v, err := takeValue(name, args, &i, inlineValue, hasInline)
			if err != nil {
				return nil, err
			}
			in.Seen[name] = true
			switch name {
			case "tag":
				in.Tags = append(in.Tags, v)
			case "untag":
				in.Untags = append(in.Untags, v)
			case "set":
				in.Sets = append(in.Sets, v)
			case "unset":
				in.Unsets = append(in.Unsets, v)
			}

		case valueModifiers[name]:
			v, err := takeValue(name, args, &i, inlineValue, hasInline)
			if err != nil {
				return nil, err
			}
			// §3.3 rule 5: last wins for everything not documented as
			// accumulating.
			in.Values[name] = v
			in.Seen[name] = true
			if name == "dir" {
				in.Dir = v
			}

		default:
			return nil, usagef("unknown switch --%s%s", name, suggest(name))
		}
	}

	if err := in.validate(); err != nil {
		return nil, err
	}
	return in, nil
}

// applyGlobalBool copies the global modifiers into their named fields, so that
// callers do not have to know they are stored as booleans.
func applyGlobalBool(in *Invocation, name string, value bool) {
	switch name {
	case "json":
		in.JSON = value
	case "porcelain":
		in.Porcelain = value
	case "dry-run":
		in.DryRun = value
	case "yes":
		in.Yes = value
	case "force":
		in.Force = value
	case "quiet":
		in.Quiet = value
	case "verbose":
		in.Verbose = value
	case "help":
		in.Help = value
	case "version":
		in.Version = value
	case "all":
		in.All = value
	}
}

// validate applies the rules that are about the invocation as a whole.
func (in *Invocation) validate() error {
	// --help and --version are answerable with no operation; nothing else is.
	if in.Op == OpNone && !in.Help && !in.Version {
		return usagef("no operation given; try --help")
	}
	if in.JSON && in.Porcelain {
		return usagef("--json and --porcelain are mutually exclusive")
	}
	// §4: --all spans every discovered directory, so naming one contradicts it.
	if in.All && in.Dir != "" {
		return usagef("--all scans for directories; it cannot be combined with --dir")
	}
	// §5.1.6: the guard exists to be deliberate. --yes must not satisfy it.
	if in.Op == OpRemove && in.Yes && !in.Force {
		return usagef("--remove requires --force; --yes does not satisfy it, " +
			"because removing an item leaves no record. To abandon work, use " +
			"--finish ID --outcome cancelled")
	}
	return nil
}

// splitSwitch parses one argument into a name and an optional inline value,
// accepting both --switch=value and the long and short prefixes.
func splitSwitch(arg string) (name, value string, hasValue bool) {
	s := strings.TrimPrefix(arg, "-")
	s = strings.TrimPrefix(s, "-")
	if s == "" {
		return "", "", false
	}
	if name, value, found := strings.Cut(s, "="); found {
		return name, value, true
	}
	return s, "", false
}

// isSwitch reports whether an argument looks like a switch rather than a value.
//
// A bare "-" is a value, and so is a negative number, so that --position -1 and
// a title of "-" are both possible.
func isSwitch(arg string) bool {
	if !strings.HasPrefix(arg, "-") || arg == "-" || arg == "--" {
		return false
	}
	rest := strings.TrimLeft(arg, "-")
	if rest == "" {
		return false
	}
	return !(rest[0] >= '0' && rest[0] <= '9')
}

// takeValue reads a switch's value from the inline form or the next argument.
func takeValue(name string, args []string, i *int, inline string, hasInline bool) (string, error) {
	if hasInline {
		return inline, nil
	}
	if *i+1 >= len(args) {
		return "", usagef("--%s needs a value", name)
	}
	next := args[*i+1]
	if isSwitch(next) {
		return "", usagef("--%s needs a value, but the next argument is %s", name, next)
	}
	*i++
	return next, nil
}

// suggest offers the closest known switch, because an unknown switch is usually
// a typo and the list of alternatives is long.
func suggest(name string) string {
	var best string
	bestScore := 3 // only suggest something genuinely close
	for _, candidate := range allSwitchNames() {
		if d := editDistance(name, candidate); d < bestScore {
			best, bestScore = candidate, d
		}
	}
	if best == "" {
		return ""
	}
	return fmt.Sprintf(" (did you mean --%s?)", best)
}

func allSwitchNames() []string {
	var out []string
	for name := range operations {
		out = append(out, name)
	}
	for name := range valueModifiers {
		out = append(out, name)
	}
	for name := range boolModifiers {
		out = append(out, name)
	}
	for name := range accumulating {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// editDistance is the Levenshtein distance, used only for the suggestion above.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, min(cur[j-1]+1, prev[j-1]+cost))
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// ParseID accepts the full form and the bare number, because typing the prefix
// is friction the format imposes for machine reasons (§3.3 rule 4).
func ParseID(s string) (mm.ID, error) {
	id, err := mm.ParseID(s)
	if err != nil {
		return "", usagef("%q is not an item id; expected T-0042, or just 42", s)
	}
	return id, nil
}
