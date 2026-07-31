package web

// Versions reported by /about (spec-gui.md §4.1) and by the health endpoint.
//
// The spec versions are the documents this implementation was written against.
// They are separate from the build version on purpose: a build that implements
// UI spec version 1 keeps saying so, whatever its own version number is.
const (
	// Version is this service's own build version.
	Version = "0.1.0-dev"

	// SpecUIVersion is spec-gui.md's declared version.
	SpecUIVersion = "1"
	// SpecToolsVersion is spec-tools.md's declared version.
	SpecToolsVersion = "1"
	// SpecFormatVersion is spec-file-format.md's declared version.
	SpecFormatVersion = "1"
)
