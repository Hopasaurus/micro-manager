package web

import (
	"embed"
	"io/fs"
)

// Static assets are embedded, so each binary is a single file with no runtime
// asset path (project/architecture.md §8). The service must work with no
// network at all - it binds to loopback - and a CDN reference would also tell a
// third party that the tool is running.

//go:embed static templates
var embedded embed.FS

// templatesFS is the tree the renderer parses. Pages sit at the root; anything
// under partials/ is shared by every page.
func templatesFS() fs.FS {
	sub, err := fs.Sub(embedded, "templates")
	if err != nil {
		panic("web: templates missing from the binary: " + err.Error())
	}
	return sub
}

// staticFS is the tree served under /static.
func staticFS() fs.FS {
	sub, err := fs.Sub(embedded, "static")
	if err != nil {
		// Unreachable: the directory is embedded at compile time, so a failure
		// here means the binary was built without it.
		panic("web: static assets missing from the binary: " + err.Error())
	}
	return sub
}
