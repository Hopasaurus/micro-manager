package web

// Versions reported by /about (spec-gui.md §4.1) and by the health endpoint.
//
// The spec versions are the documents this implementation was written against.
// They are separate from the build version on purpose: a build that implements
// UI spec version 1 keeps saying so, whatever its own version number is.
// Version is replaced from the repository VERSION file by supported builds.
// Direct go build/go run invocations deliberately identify themselves as dev.
var Version = "0.0.0-dev"

const (
	// SpecUIVersion is spec-gui.md's declared version.
	SpecUIVersion = "2"
	// SpecToolsVersion is spec-tools.md's declared version.
	SpecToolsVersion = "2"
	// SpecFormatVersion is spec-file-format.md's declared version.
	SpecFormatVersion = "2"
)
