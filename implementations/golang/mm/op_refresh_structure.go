package mm

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	structureRefreshChecked = "- [x] Allow `mm --refresh-structure` to update generated documentation."
	structureRefreshOpen    = "<!-- mm:user-notes:begin -->"
	structureRefreshClose   = "<!-- mm:user-notes:end -->"
)

// RefreshStructureRequest controls regeneration of structure.md.
type RefreshStructureRequest struct{ DryRun bool }

// RefreshStructureResult reports whether structure.md was created or updated.
type RefreshStructureResult struct {
	Path    string `json:"path"`
	Created bool   `json:"created"`
	Updated bool   `json:"updated"`
}

// RefreshStructure explicitly regenerates the managed portions of
// structure.md while preserving its description and delimited user notes.
func (s *Store) RefreshStructure(req RefreshStructureRequest, today Date) (RefreshStructureResult, TxResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.load()
	if err != nil {
		return RefreshStructureResult{}, TxResult{}, err
	}
	if err := refuseIfV1(m); err != nil {
		return RefreshStructureResult{}, TxResult{}, err
	}
	path := filepath.Join(s.path, "structure.md")
	was := stampOf(path)
	original, err := os.ReadFile(path)
	missing := os.IsNotExist(err)
	if err != nil && !missing {
		return RefreshStructureResult{}, TxResult{}, fmt.Errorf("%w: reading structure.md: %v", ErrIO, err)
	}

	description := ""
	notes := defaultStructureNotes()
	if !missing {
		if countExactLines(string(original), structureRefreshChecked) != 1 ||
			bytes.Count(original, []byte(structureRefreshChecked)) != 1 ||
			bytes.Contains(original, []byte("- [ ] Allow `mm --refresh-structure`")) {
			return RefreshStructureResult{}, TxResult{}, fmt.Errorf("%w: structure.md is protected; keep exactly one checked refresh marker or rename the file", ErrPreconditionFailed)
		}
		var ok bool
		notes, ok = structureUserNotes(string(original))
		if !ok {
			return RefreshStructureResult{}, TxResult{}, fmt.Errorf("%w: structure.md user-note boundaries are missing, duplicated, or out of order", ErrPreconditionFailed)
		}
		description = structureDescription(string(original))
	}
	project := m.board.FM.Get("project")
	content := []byte(renderManagedStructureV2(project, today, description, m.grammar(), notes))
	result := RefreshStructureResult{Path: "structure.md", Created: missing, Updated: !missing && !bytes.Equal(original, content)}
	tx := TxResult{DryRun: req.DryRun}
	if !missing && bytes.Equal(original, content) {
		return result, tx, nil
	}
	tx.Files = []string{"structure.md"}
	kind := ChangeUpdated
	if missing {
		kind = ChangeCreated
	}
	tx.Changes = []Change{{Kind: kind, File: "structure.md"}}
	if req.DryRun {
		return result, tx, nil
	}
	ws := writeSet{}
	ws.Add(path, content, was)
	if err := ws.Commit(); err != nil {
		return RefreshStructureResult{}, tx, err
	}
	return result, tx, nil
}

func structureUserNotes(s string) (string, bool) {
	if countExactLines(s, structureRefreshOpen) != 1 || countExactLines(s, structureRefreshClose) != 1 ||
		strings.Count(s, structureRefreshOpen) != 1 || strings.Count(s, structureRefreshClose) != 1 {
		return "", false
	}
	start := strings.Index(s, structureRefreshOpen) + len(structureRefreshOpen)
	end := strings.Index(s, structureRefreshClose)
	if end < start {
		return "", false
	}
	return s[start:end], true
}

func countExactLines(s, marker string) int {
	count := 0
	for _, line := range strings.Split(s, "\n") {
		if line == marker {
			count++
		}
	}
	return count
}

func structureDescription(s string) string {
	lines := strings.Split(s, "\n")
	i := 0
	if len(lines) > 0 && lines[0] == "---" {
		for i = 1; i < len(lines) && lines[i] != "---"; i++ {
		}
		if i < len(lines) {
			i++
		}
	}
	for i < len(lines) && (strings.TrimSpace(lines[i]) == "" || strings.HasPrefix(lines[i], "#")) {
		i++
	}
	start := i
	for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
		i++
	}
	return strings.Join(lines[start:i], "\n")
}

func defaultStructureNotes() string {
	return "\n## User notes\n\nAdd board-specific notes here. `mm --refresh-structure` preserves everything\nbetween these markers exactly.\n"
}

func renderManagedStructureV2(project string, today Date, description string, grammar IDGrammar, notes string) string {
	if description == "" {
		description = "A plain-Markdown task board, readable without special tooling."
	}
	example := grammar.NewID(42)
	return fmt.Sprintf(`---
doc: structure
version: 2
updated: %s
---

# %s

%s

%s

## Authority

This file is read-only orientation: it is optional documentation, not board
state, and validators do not parse it. Careful manual edits to the data files
are supported, but `+"`mm`"+` is safer because it preserves cross-file invariants.

## Files

| Path | Purpose |
|---|---|
| `+"`board.md`"+` | Open items in one ordered list; each item carries `+"`stage:`"+`. |
| `+"`done.md`"+` | Finished and cancelled items grouped by month. |
| `+"`details/<ID>.md`"+` | Optional durable description for one item. |
| `+"`done-YYYY.md`"+`, `+"`details-YYYY/`"+` | Optional archives, outside validation. |
| `+"`audit.md`"+` | Optional append-only action log. |

## Item lines

`+"```text"+`
- [ ] [%s] Fix the deploy script | stage:ready | prio:high | tags:infra,ci | created:2026-07-29
`+"```"+`

Open items use `+"`- [ ]`"+`; done items use `+"`- [x]`"+`. Fields are
`+"` | `"+`-separated `+"`key:value`"+` pairs. Unknown fields are legal and must survive
moves. Stages and any `+"`wip.<stage>`"+` caps are declared in `+"`board.md`"+` frontmatter.

## Safe manual edits

- An ID has exactly one home: `+"`board.md`"+` or `+"`done.md`"+`. Move; never copy.
- `+"`next_id`"+` only rises. IDs are permanent and never reused or renumbered.
- Renaming an item also requires the matching detail `+"`title`"+` to change.
- A detail path, filename, frontmatter `+"`id`"+`, and owning item ID must agree.
- Preserve unknown fields. Tags contain no spaces; dates are real ISO dates.
- Run `+"`mm --check`"+` after editing data files directly.

## Commands and more help

Use `+"`mm --help`"+` for the current command list and
`+"`mm --help --OPERATION`"+` for one operation. This file explains storage;
help explains actions.

Normative documentation:

- Repository: https://github.com/Hopasaurus/micro-manager
- Format: `+"`project/spec-file-format.md`"+`
- Tools: `+"`project/spec-tools.md`"+`

When this overview and a specification disagree, the format specification wins.

%s%s
`, today.String(), project, description, structureRefreshChecked, example, structureRefreshOpen, notes+structureRefreshClose)
}
