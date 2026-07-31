package web

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"micromanager/mm"
)

// Error mapping (spec-gui.md §4.3).
//
// One table, one place. This is the UI's equivalent of internal/cli/exit.go, and
// it is written the same way for the same reason: an errors.Is chain scattered
// through handlers drifts, and three distinct library errors all map to 409 here
// so the STATUS alone cannot tell a client what happened.
//
// The body therefore carries the error code verbatim, and a client branches on
// the name rather than the status.

// Error codes are the spec's names (spec-tools.md §6.3), not the Go sentinels'
// message text. The external suite branches on these strings.
const (
	codeNotFound           = "NotFound"
	codeAmbiguous          = "Ambiguous"
	codeInvalidArgument    = "InvalidArgument"
	codeConflict           = "Conflict"
	codeAlreadyExists      = "AlreadyExists"
	codeWipLimitReached    = "WipLimitReached"
	codePreconditionFailed = "PreconditionFailed"
	codeInvariantViolation = "InvariantViolation"
	codeConcurrent         = "Concurrent"
	codeIO                 = "Io"
	codeInternal           = "Internal"
)

// apiError is one entry in the error envelope of §4.3.
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	ID      string `json:"id,omitempty"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`

	// Invariant is the I1-I10 identifier, present on an InvariantViolation so
	// the check view can render data-invariant without a second lookup.
	Invariant string `json:"invariant,omitempty"`
}

// errorEnvelope is the body of §4.3: { "ok": false, "errors": [ ... ] }.
type errorEnvelope struct {
	OK     bool       `json:"ok"`
	Errors []apiError `json:"errors"`

	// WipUsed, WipLimit and Occupants are carried on a WipLimitReached, which
	// §4.3 singles out because it is the one error with a routine remedy.
	WipUsed   int        `json:"wipUsed,omitempty"`
	WipLimit  int        `json:"wipLimit,omitempty"`
	Occupants []occupant `json:"occupants,omitempty"`

	// Candidates are carried on an Ambiguous, which §4.3 requires to list them.
	Candidates []string `json:"candidates,omitempty"`
}

// occupant is one working slot in a WipLimitReached body, and the data
// dialog-wip-limit renders its slot rows from (§5.10).
type occupant struct {
	Slot  int    `json:"slot"`
	ID    string `json:"id"`
	Title string `json:"title"`
}

// statusFor maps a library error to its HTTP status (spec-gui.md §4.3).
//
// Order matters: the typed errors are checked before the sentinels they wrap,
// and ErrWipLimitReached before ErrConflict, because §4.3 gives them different
// bodies even though both are 409.
func statusFor(err error) (int, string) {
	switch {
	case err == nil:
		return http.StatusOK, ""
	case errors.Is(err, mm.ErrNotFound):
		return http.StatusNotFound, codeNotFound
	case errors.Is(err, mm.ErrAmbiguous):
		return http.StatusConflict, codeAmbiguous
	case errors.Is(err, mm.ErrWipLimitReached):
		return http.StatusConflict, codeWipLimitReached
	case errors.Is(err, mm.ErrInvariantViolation):
		return http.StatusUnprocessableEntity, codeInvariantViolation
	case errors.Is(err, mm.ErrConcurrent):
		return http.StatusConflict, codeConcurrent
	case errors.Is(err, mm.ErrPreconditionFailed):
		return http.StatusPreconditionFailed, codePreconditionFailed
	case errors.Is(err, mm.ErrAlreadyExists):
		return http.StatusConflict, codeAlreadyExists
	case errors.Is(err, mm.ErrConflict):
		return http.StatusConflict, codeConflict
	case errors.Is(err, mm.ErrInvalidArgument):
		return http.StatusBadRequest, codeInvalidArgument
	case errors.Is(err, mm.ErrIO):
		return http.StatusInternalServerError, codeIO
	}
	return http.StatusInternalServerError, codeInternal
}

// envelopeFor builds the §4.3 body for an error.
func envelopeFor(err error) errorEnvelope {
	_, code := statusFor(err)

	env := errorEnvelope{
		OK:     false,
		Errors: []apiError{{Code: code, Message: cleanMessage(err)}},
	}

	// WipLimitReached carries the limit and the occupants, because the dialog
	// that renders it offers finish, pause and raise-limit against those very
	// slots - the same three remedies the CLI names.
	if wip, ok := mm.AsWipLimitError(err); ok {
		env.WipUsed = wip.Limit
		env.WipLimit = wip.Limit
		for _, slot := range wip.Occupants {
			if slot.Item == nil {
				continue
			}
			env.Occupants = append(env.Occupants, occupant{
				Slot:  slot.Number,
				ID:    string(slot.Item.ID),
				Title: slot.Item.Title,
			})
		}
		env.WipUsed = len(env.Occupants)
	}

	// InvariantViolation carries the violation list, in the shape the check view
	// renders: data-invariant, data-file, data-line.
	if inv, ok := mm.AsInvariantError(err); ok {
		env.Errors = env.Errors[:0]
		for _, v := range inv.Violations {
			env.Errors = append(env.Errors, apiError{
				Code:      codeInvariantViolation,
				Message:   v.Message,
				Invariant: v.Invariant,
				File:      v.At.File,
				Line:      v.At.Line,
			})
		}
		if len(env.Errors) == 0 {
			env.Errors = []apiError{{Code: codeInvariantViolation, Message: cleanMessage(err)}}
		}
	}

	return env
}

// cleanMessage strips the sentinel's own text from the front of a wrapped
// error, so a body reads "T-0042 is not in this directory" rather than
// "not found: T-0042 is not in this directory". The code already says which
// error it is.
func cleanMessage(err error) string {
	msg := err.Error()
	for _, prefix := range []string{
		"not found: ", "ambiguous: ", "invalid argument: ", "conflict: ",
		"wip limit reached: ", "precondition failed: ", "invariant violation: ",
		"concurrent modification: ", "already exists: ", "i/o error: ",
	} {
		if strings.HasPrefix(msg, prefix) {
			return strings.TrimPrefix(msg, prefix)
		}
	}
	return msg
}

// errorHandler is Echo's single error handler.
//
// The body form depends on the caller, not on the error: JSON under /api/v1, an
// HTML fragment for an htmx request, a full error page otherwise (§4.3,
// project/architecture.md §4.8). The code string appears in every form.
func (s *Server) errorHandler(c *echo.Context, err error) {
	if res, ok := c.Response().(*echo.Response); ok && res.Committed {
		// Something already wrote. Appending an error to a half-written response
		// would corrupt it; the client will see a truncated body, which is the
		// least bad outcome available.
		s.log.Error("error after the response was committed", "path", c.Path(), "error", err)
		return
	}

	status, code := statusFor(err)
	env := envelopeFor(err)

	// A framework error - the router's 404, or the guards' 421 and 403 - carries
	// its own status. It is asked for through echo.StatusCode rather than a type
	// assertion, because v5's router errors are an UNEXPORTED type implementing
	// HTTPStatusCoder: errors.As(&echo.HTTPError{}) does not see them, and a 404
	// silently became a 500 until this test caught it.
	if frameworkStatus := echo.StatusCode(err); frameworkStatus != 0 {
		status = frameworkStatus
		code = httpErrorCode(frameworkStatus)
		message := err.Error()
		var he *echo.HTTPError
		if errors.As(err, &he) && he.Message != "" {
			message = he.Message
		}
		env = errorEnvelope{OK: false, Errors: []apiError{{Code: code, Message: message}}}
	}

	if status >= http.StatusInternalServerError {
		s.log.Error("request failed", "path", c.Request().URL.Path, "status", status, "error", err)
	}

	if renderErr := s.writeError(c, status, code, env); renderErr != nil {
		s.log.Error("could not write the error response", "error", renderErr)
	}
}

func (s *Server) writeError(c *echo.Context, status int, code string, env errorEnvelope) error {
	req := c.Request()

	if strings.HasPrefix(req.URL.Path, "/api/") {
		return c.JSON(status, env)
	}

	// An htmx request gets a fragment it can swap: a dialog where the error has
	// one, a toast otherwise. The code goes on the element so a test can branch
	// on the name rather than the status (§5.10, §7.5).
	if req.Header.Get("HX-Request") == "true" {
		return s.writeErrorFragment(c, status, code, env)
	}

	// A whole page, in the app shell, so an error is a place the user can
	// navigate out of rather than a wall of JSON.
	v := s.newView(c, "Error", nil)
	v.Data = map[string]any{"Code": code, "Message": firstMessage(env), "Severity": "danger"}
	if err := s.render(c, status, "error", "", v); err == nil {
		return nil
	}
	return c.JSON(status, env)
}

// writeErrorFragment is the htmx path. The dialogs of §5.10 are added by the
// items that own them; this chooses which one an error asks for and falls back
// to a toast.
func (s *Server) writeErrorFragment(c *echo.Context, status int, code string, env errorEnvelope) error {
	fragment := "toast"
	switch code {
	case codeWipLimitReached:
		fragment = "dialog-wip-limit"
	case codeConcurrent:
		fragment = "dialog-conflict"
	}

	v := s.newView(c, "Error", nil)
	v.Data = map[string]any{
		"Code":     code,
		"Status":   status,
		"Severity": "danger",
		"Envelope": env,
		"Message":  firstMessage(env),
	}
	if err := s.render(c, status, "error", fragment, v); err == nil {
		return nil
	}
	// No template for it yet. A bare fragment carrying the code still lets the
	// client behave correctly, which matters more than how it looks.
	return c.HTML(status, fmt.Sprintf(
		`<div data-testid="toast-error" data-severity="danger" data-code="%s">%s</div>`,
		template.HTMLEscapeString(code), template.HTMLEscapeString(firstMessage(env))))
}

func firstMessage(env errorEnvelope) string {
	if len(env.Errors) == 0 {
		return ""
	}
	return env.Errors[0].Message
}

// httpErrorCode names the framework's own refusals, so a client branches on a
// name for those too.
func httpErrorCode(status int) string {
	switch status {
	case http.StatusNotFound:
		return codeNotFound
	case http.StatusMisdirectedRequest:
		return "HostNotAllowed"
	case http.StatusForbidden:
		return "OriginNotAllowed"
	case http.StatusMethodNotAllowed:
		return "MethodNotAllowed"
	case http.StatusBadRequest:
		return codeInvalidArgument
	}
	if status >= 500 {
		return codeInternal
	}
	return codeConflict
}
