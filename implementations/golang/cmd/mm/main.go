// Command mm is the micro-manager command line tool.
package main

import (
	"os"

	"micromanager/internal/cli"
)

func main() {
	os.Exit(cli.Run(cli.Env{
		Args:   os.Args[1:],
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Dir:    os.Getenv("MM_DIR"),
	}))
}
