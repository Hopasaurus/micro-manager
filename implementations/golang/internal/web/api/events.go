package api

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/Hopasaurus/micro-manager/mm"
)

// The event stream (spec-gui.md §2.3, project/architecture.md §4.5).
//
// Events carry NOTIFICATIONS, not content: the body is a name, and the client
// re-fetches the region over its normal route. That is what makes "works with
// polling alone" true by construction — the same element, the same route, only
// the trigger differs — and it keeps multi-line HTML fragments out of SSE's
// one-line-per-data framing.

// Event is one named notification on the stream. Names are the small closed
// set of architecture.md §4.5: board, status, check, theme.
type Event struct {
	Name string
}

// events serves GET /api/v1/events: one stream per browser tab, scoped to the
// project named by ?project=<projectId>.
//
// Three things keep the stream alive; each is a failure that looks like a
// flaky client if missed:
//
//  1. The server's WriteTimeout must be 0, set in web.Server.Start
//     (architecture.md §4.5 item 1).
//  2. Gzip middleware must skip this route — set in web.Server.New
//     (item 2).
//  3. Every event is flushed, via http.NewResponseController (item 3).
func (s *Server) events(c *echo.Context) error {
	projectID := c.QueryParam("project")
	if projectID == "" {
		return fmt.Errorf("%w: the events stream needs ?project=<projectId>", mm.ErrInvalidArgument)
	}

	w := c.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	sub, unsubscribe := s.svc.Subscribe(projectID)
	defer unsubscribe()

	rc := http.NewResponseController(w)
	// Send the response head NOW. Until the first event the handler has nothing
	// to write, and an EventSource that never sees a response head stays
	// "connecting" forever - so does any client that waits for headers before
	// reading. A comment line is the SSE idiom for "connected, nothing yet".
	if _, err := fmt.Fprintf(w, ": connected\n\n"); err != nil {
		return nil
	}
	rc.Flush()

	for {
		select {
		case ev, ok := <-sub:
			if !ok {
				// Dropped for being too slow to read; the client's EventSource
				// reconnects with a fresh buffer.
				return nil
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: {}\n\n", ev.Name); err != nil {
				return nil
			}
			rc.Flush()
		case <-c.Request().Context().Done():
			return nil // the tab went away
		case <-s.svc.Done():
			// The service is stopping. Returning lets the connection go idle
			// so echo's graceful shutdown completes instead of waiting out its
			// whole timeout (T-0151); a stream that only watched the request
			// context stayed open until the deadline and logged "failed to
			// shut down server within given timeout".
			return nil
		}
	}
}
