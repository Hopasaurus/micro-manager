package web

import (
	"net/http"
	"net/url"
	"testing"
)

// The audit log view (spec-gui.md §5.11, T-0253).

func TestAuditViewUnavailableOnVersion1(t *testing.T) {
	ts, id := boardServer(t, "clean-full")
	body := ts.get("/p/" + id + "/audit").expectStatus(http.StatusOK).Body
	if !hasTestid(body, "audit-unavailable") {
		t.Errorf("a version-1 project should render audit-unavailable:\n%s", body)
	}
	if hasTestid(body, "audit-entries") || hasTestid(body, "audit-disabled") {
		t.Errorf("a version-1 project should render nothing else:\n%s", body)
	}
}

func TestAuditViewDisabledByDefault(t *testing.T) {
	ts, id := boardServer(t, "clean-v2-full")
	body := ts.get("/p/" + id + "/audit").expectStatus(http.StatusOK).Body
	if !hasTestid(body, "audit-disabled") {
		t.Errorf("audit should render as disabled by default:\n%s", body)
	}
}

func TestAuditViewListsEntriesNewestFirst(t *testing.T) {
	ts, id := boardServer(t, "clean-v2-full")

	ts.form(http.MethodPost, "/p/"+id+"/settings", url.Values{"audit": {"on"}}).
		expectStatus(http.StatusOK)

	// T-0001 (ready) -> working; wip.working:2 and T-0003 already occupies
	// one slot, so this logs stage and started for T-0001.
	ts.post("/p/"+id+"/items/T-0001/start", "{}", "HX-Request", "true").
		expectStatus(http.StatusOK)

	body := ts.get("/p/" + id + "/audit").expectStatus(http.StatusOK).Body
	if hasTestid(body, "audit-empty") || hasTestid(body, "audit-disabled") {
		t.Errorf("entries should have been logged:\n%s", body)
	}
	if !hasTestid(body, "audit-entries") {
		t.Fatalf("audit-entries table missing:\n%s", body)
	}

	// Newest first: the started: line (recorded after stage:) is entry 0.
	first := testid(t, body, "audit-entry-0")
	if attrOf(t, first, "data-item-id") != "T-0001" {
		t.Errorf("entry 0 item = %q, want T-0001:\n%s", attrOf(t, first, "data-item-id"), body)
	}
}

func TestAuditViewEmptyWhenEnabledButUntouched(t *testing.T) {
	ts, id := boardServer(t, "clean-v2-full")
	ts.form(http.MethodPost, "/p/"+id+"/settings", url.Values{"audit": {"on"}}).
		expectStatus(http.StatusOK)

	body := ts.get("/p/" + id + "/audit").expectStatus(http.StatusOK).Body
	if !hasTestid(body, "audit-empty") {
		t.Errorf("a freshly-enabled, untouched log should render audit-empty:\n%s", body)
	}
}
