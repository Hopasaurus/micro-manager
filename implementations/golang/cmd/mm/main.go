// Command mm is the micro-manager command line tool.
//
// This file is the ONLY place in the program that touches process state: argv,
// the environment, the working directory, the clock and the exit code. Every one
// of them is resolved into a value on cli.Env and passed inward, so that the
// wrapper is testable and the library never sees any of it (spec-tools.md §2.2).
package main

import (
	"os"
	"time"

	"github.com/Hopasaurus/micro-manager/internal/cli"
	"github.com/Hopasaurus/micro-manager/mm"
)

// isTerminal reports whether a file is a character device, which is the cheap
// portable test for "a person is on the other end". A pipe or a CI job is not,
// and launching an editor there would hang forever.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		// Not fatal on its own: --dir or MM_DIR can still name a directory, and
		// --help and --version need nothing at all.
		cwd = ""
	}

	now := time.Now()
	os.Exit(cli.Run(cli.Env{
		Args:         os.Args[1:],
		Stdout:       os.Stdout,
		Stderr:       os.Stderr,
		Stdin:        os.Stdin,
		Dir:          os.Getenv("MM_DIR"),
		ReportPeriod: os.Getenv("MM_REPORT_PERIOD"),
		Cwd:          cwd,
		// The local date, not UTC: "today" means the user's today. The clock is
		// read once, here, so a run that straddles midnight still stamps one
		// date on everything it writes.
		Today:   mm.Date{Year: now.Year(), Month: int(now.Month()), Day: now.Day()},
		NoColor: os.Getenv("NO_COLOR") != "",
		Editor:  os.Getenv("EDITOR"),
		Visual:  os.Getenv("VISUAL"),
		// Only hand the terminal to an editor when there is one to hand over.
		Interactive: isTerminal(os.Stdin) && isTerminal(os.Stdout),
	}))
}
