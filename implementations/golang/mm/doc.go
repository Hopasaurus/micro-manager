// Package mm implements micro-manager: reading, validating and writing the
// plain-markdown todo directories specified in project/spec-file-format.md,
// and the operations specified in project/spec-tools.md.
//
// This package is the whole product. The command line tool, the UI service and
// the terminal UI each compile it in and add only presentation. It is therefore
// a guest in a process it does not own, and observes the constraints in
// spec-tools.md §2.2: it never writes to stdout or stderr, never terminates the
// process, never reads argv, the environment or the working directory, holds no
// global mutable state, and installs no signal handlers. Everything an
// operation needs arrives as a parameter; everything it reports comes back as a
// value.
package mm
