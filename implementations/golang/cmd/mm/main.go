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

	"micromanager/internal/cli"
	"micromanager/mm"
)

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
		Dir:          os.Getenv("MM_DIR"),
		ReportPeriod: os.Getenv("MM_REPORT_PERIOD"),
		Cwd:          cwd,
		// The local date, not UTC: "today" means the user's today. The clock is
		// read once, here, so a run that straddles midnight still stamps one
		// date on everything it writes.
		Today:   mm.Date{Year: now.Year(), Month: int(now.Month()), Day: now.Day()},
		NoColor: os.Getenv("NO_COLOR") != "",
	}))
}
