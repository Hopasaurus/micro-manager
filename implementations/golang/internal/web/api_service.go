package web

import (
	"time"

	"micromanager/internal/web/api"
	"micromanager/mm"
)

// The JSON API's view of the web server (internal/web/api.Service).
//
// These are thin delegations to the registry and the options, so the API
// package never reaches into unexported state. Nothing here is a rule; the
// rules live in mm, and this file exists only to draw the seam between the two
// handler sets (project/architecture.md §4.7).

// ResolveProject turns a projectId into an open store.
func (s *Server) ResolveProject(id string) (*mm.Store, error) {
	return s.registry.resolve(id)
}

// ProjectDirectory returns what is known about a project without opening it.
func (s *Server) ProjectDirectory(id string) (mm.Directory, bool) {
	return s.registry.directory(id)
}

// DiscoveryRaw is the cached discovery result, scanning on first use.
func (s *Server) DiscoveryRaw() (mm.DiscoveryResult, mm.Timestamp) {
	return s.registry.rawResult()
}

// RescanRaw re-runs discovery and returns the raw result.
func (s *Server) RescanRaw() (mm.DiscoveryResult, mm.Timestamp) {
	return s.registry.rescanRaw()
}

// Lists loads the recent and favorites files.
func (s *Server) Lists() (recent, favorites *mm.ProjectList, err error) {
	return s.registry.lists()
}

// Config is the merged configuration the service started with.
func (s *Server) Config() mm.Config { return s.opts.Config }

// ConfigHome is the resolved configuration home.
func (s *Server) ConfigHome() string { return s.opts.ConfigHome }

// SystemConfig is the system config FILE, or nil.
func (s *Server) SystemConfig() *mm.ConfigFile { return s.opts.SystemConfig }

// Now is the injectable clock.
func (s *Server) Now() time.Time { return s.registry.now() }

// Subscribe opens a per-project event stream (api.Service).
func (s *Server) Subscribe(projectID string) (<-chan api.Event, func()) {
	return s.broker.Subscribe(projectID)
}

// Done is closed when the service is told to stop (api.Service).
func (s *Server) Done() <-chan struct{} { return s.shutdown }
