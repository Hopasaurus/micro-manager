package web

import (
	"context"
	"log/slog"
	"testing"
)

// newTestLogger hands the service a logger whose records go to t.Log, so a
// warning raised during a test is attached to that test instead of scrolling
// past in the terminal.
func newTestLogger(t testing.TB) *slog.Logger {
	return slog.New(&testHandler{t: t})
}

type testHandler struct {
	t     testing.TB
	attrs []slog.Attr
}

func (h *testHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *testHandler) Handle(_ context.Context, r slog.Record) error {
	h.t.Helper()
	msg := r.Level.String() + " " + r.Message
	for _, a := range h.attrs {
		msg += " " + a.String()
	}
	r.Attrs(func(a slog.Attr) bool {
		msg += " " + a.String()
		return true
	})
	h.t.Log(msg)
	return nil
}

func (h *testHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &testHandler{t: h.t, attrs: append(append([]slog.Attr(nil), h.attrs...), attrs...)}
}

func (h *testHandler) WithGroup(string) slog.Handler { return h }
