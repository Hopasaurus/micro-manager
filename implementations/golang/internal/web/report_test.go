package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Hopasaurus/micro-manager/mm"
)

// reportServer pins the clock, because every period this view resolves is
// relative to today and a test that drifts with the calendar is not a test.
func reportServer(t *testing.T, fixture string) (*testServer, string) {
	t.Helper()
	ts, id := boardServer(t, fixture)
	ts.registry.now = func() time.Time { return time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC) }
	return ts, id
}

// §5.7 fixes these testids and these attributes.
func TestReportView(t *testing.T) {
	ts, id := reportServer(t, "clean-full")
	body := ts.get("/p/" + id + "/report?period=all").expectStatus(http.StatusOK).Body

	for _, testid := range []string{
		"report", "report-controls", "report-period", "report-since",
		"report-until", "report-group-by", "report-include-wip",
		"report-body", "report-copy",
	} {
		if !hasTestid(body, testid) {
			t.Errorf("the report is missing data-testid=%q", testid)
		}
	}

	report := testid(t, body, "report")
	for _, attr := range []string{"data-period", "data-period-source", "data-count"} {
		if attrOf(t, report, attr) == "" {
			t.Errorf("report carries no %s: %s", attr, report)
		}
	}

	// data-from and data-to carry the resolved range. "all" is unbounded and
	// legitimately has neither, so a bounded period is what proves they are set.
	bounded := ts.get("/p/" + id + "/report?period=this-week").Body
	tag := testid(t, bounded, "report")
	if attrOf(t, tag, "data-from") == "" || attrOf(t, tag, "data-to") == "" {
		t.Errorf("a bounded period carries no range: %s", tag)
	}
}

// §5.7: data-period-source MUST be one of switch, config or default, mirroring
// the precedence of spec-tools.md §5.1.11. The resolved period must be visible.
func TestReportPeriodPrecedence(t *testing.T) {
	cases := []struct {
		name       string
		query      string
		configured string
		want       string
	}{
		{name: "nothing asked for", want: "default"},
		{name: "the period parameter", query: "?period=this-week", want: "switch"},
		{name: "an explicit range", query: "?since=2026-07-01&until=2026-07-15", want: "switch"},
		{name: "an open-ended range", query: "?since=2026-07-01", want: "switch"},
		{name: "config, with nothing asked for", configured: "this-week", want: "config"},
		{name: "a parameter beats config", query: "?period=last-week", configured: "this-week", want: "switch"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ts, id := reportServer(t, "clean-full")
			if c.configured != "" {
				f, err := mm.LoadConfigFile(mm.NewSystemPaths(ts.ConfigHome).Config, mm.ScopeSystem)
				if err != nil {
					t.Fatal(err)
				}
				if err := f.Set("report.period", c.configured); err != nil {
					t.Fatal(err)
				}
				ts.opts.SystemConfig = f
			}

			body := ts.get("/p/" + id + "/report" + c.query).expectStatus(http.StatusOK).Body
			report := testid(t, body, "report")
			if got := attrOf(t, report, "data-period-source"); got != c.want {
				t.Errorf("data-period-source = %q, want %q", got, c.want)
			}
		})
	}
}

// §9.2: a UI service MUST NOT read MM_REPORT_PERIOD from its own environment.
// The service outlives any one user's shell, so inheriting it is the coupling
// spec-tools.md §3.5 exists to prevent.
func TestReportIgnoresTheEnvironment(t *testing.T) {
	t.Setenv("MM_REPORT_PERIOD", "this-week")

	ts, id := reportServer(t, "clean-full")
	body := ts.get("/p/" + id + "/report").Body
	report := testid(t, body, "report")

	if got := attrOf(t, report, "data-period-source"); got != "default" {
		t.Errorf("data-period-source = %q; the environment reached a UI service", got)
	}
	if got := attrOf(t, report, "data-period"); strings.Contains(got, "this-week") {
		t.Errorf("data-period = %q; the environment reached a UI service", got)
	}
}

// §5.7: report-copy MUST place the same paste-ready markdown the CLI produces
// on the clipboard. One renderer in the library is how that is guaranteed.
func TestReportCopyMatchesTheCLI(t *testing.T) {
	ts, id := reportServer(t, "clean-full")
	body := ts.get("/p/" + id + "/report?period=all").Body

	store, err := mm.Open(ts.Dirs[0])
	if err != nil {
		t.Fatal(err)
	}
	period, err := mm.ParsePeriod("all", mm.Date{Year: 2026, Month: 7, Day: 30})
	if err != nil {
		t.Fatal(err)
	}
	period.Source = mm.PeriodFromSwitch
	rep, err := store.Report(period, mm.ReportOptions{})
	if err != nil {
		t.Fatal(err)
	}

	want := rep.Markdown()
	if want == "" {
		t.Fatal("the library rendered nothing")
	}
	// The markup carries it escaped; unescaping the few entities html/template
	// produces is enough to compare the text a user would paste.
	got := unescapeHTML(betweenTags(t, body, `<pre data-testid="report-markdown"`, "</pre>"))
	if got != want {
		t.Errorf("report-copy would paste different text from the CLI:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	if !strings.Contains(testid(t, body, "report-copy"), "data-clipboard") {
		t.Error("report-copy carries no text to copy")
	}
}

// §5.7 groups the body, and the DOM shape must not depend on whether grouping
// was asked for: a suite asserting report-group-* finds one either way.
func TestReportGrouping(t *testing.T) {
	ts, id := reportServer(t, "clean-full")

	ungrouped := ts.get("/p/" + id + "/report?period=all").Body
	if !strings.Contains(ungrouped, `data-testid="report-group-`) {
		t.Error("an ungrouped report has no group at all")
	}

	grouped := ts.get("/p/" + id + "/report?period=all&group-by=outcome").Body
	if !hasTestid(grouped, "report-group-shipped") {
		t.Errorf("grouping by outcome produced no shipped group")
	}

	// Every item carries its outcome and its date as attributes (§5.7).
	item := testid(t, grouped, "report-item-T-0008")
	if got := attrOf(t, item, "data-outcome"); got == "" {
		t.Errorf("a report item carries no data-outcome: %s", item)
	}
	if got := attrOf(t, item, "data-done"); got == "" {
		t.Errorf("a report item carries no data-done: %s", item)
	}
}

// A period with nothing in it is a legitimate answer, not an error.
func TestEmptyReport(t *testing.T) {
	ts, id := reportServer(t, "clean-full")
	body := ts.get("/p/" + id + "/report?since=2020-01-01&until=2020-01-07").
		expectStatus(http.StatusOK).Body

	report := testid(t, body, "report")
	if got := attrOf(t, report, "data-count"); got != "0" {
		t.Errorf("data-count = %q", got)
	}
	if got := attrOf(t, report, "data-state"); got != "empty" {
		t.Errorf("data-state = %q, want empty", got)
	}
	if !hasTestid(body, "report-empty") {
		t.Error("an empty report says nothing to the user")
	}
}

// A bad period is a 400 naming the value, not a silent fallback: a typo that
// quietly changed the period would make every report unreliable.
func TestBadPeriodIsRefused(t *testing.T) {
	ts, id := reportServer(t, "clean-full")

	r := ts.get("/p/" + id + "/report?period=last-fortnight")
	if r.Status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", r.Status)
	}
	if !strings.Contains(r.Body, "last-fortnight") {
		t.Errorf("the refusal does not name the value: %s", r.Body)
	}

	if got := ts.get("/p/" + id + "/report?since=2026-02-31").Status; got != http.StatusBadRequest {
		t.Errorf("an impossible date returned %d", got)
	}
}

// §4.1 rule 2: loading the URI directly produces the same state as navigating
// to it, so the controls reflect the query.
func TestReportControlsReflectTheQuery(t *testing.T) {
	ts, id := reportServer(t, "clean-full")
	body := ts.get("/p/" + id + "/report?period=this-week&group-by=outcome&include-wip=true").Body

	if !strings.Contains(testid(t, body, "report-include-wip"), "checked") {
		t.Error("include-wip is not reflected in the form")
	}
	if !strings.Contains(body, `<option value="this-week" selected>`) {
		t.Error("the period is not selected in the form")
	}
	if !strings.Contains(body, `<option value="outcome" selected>`) {
		t.Error("the grouping is not selected in the form")
	}
	if !hasTestid(body, "report-group-in-progress") {
		t.Error("include-wip produced no in-progress group")
	}
}

// betweenTags returns the text between an opening tag and a closing one.
func betweenTags(t *testing.T, body, open, close string) string {
	t.Helper()
	i := strings.Index(body, open)
	if i < 0 {
		t.Fatalf("no %s in the document", open)
	}
	rest := body[i:]
	j := strings.Index(rest, ">")
	k := strings.Index(rest, close)
	if j < 0 || k < 0 {
		t.Fatalf("%s is not closed", open)
	}
	return rest[j+1 : k]
}

func unescapeHTML(s string) string {
	return strings.NewReplacer(
		"&amp;", "&", "&lt;", "<", "&gt;", ">", "&#34;", `"`, "&#39;", "'", "&nbsp;", " ",
	).Replace(s)
}
