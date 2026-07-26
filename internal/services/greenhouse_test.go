package services

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestGreenhouse(t *testing.T) {
	testSingle(t, "tailscale", Greenhouse)
}

func TestGreenhouse_all(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	testMultipleParallel(t, slices.Values(GreenhouseCompanies), Greenhouse)
}

// greenhouseEnrichedBoard is one response in the shape Greenhouse already serves
// for the list URL this adapter already fetches, without `?content=true`.
//
// requisition_id deliberately arrives as a string, as a bare number and as null
// in the same page, because that is what Greenhouse actually does and a decoder
// that assumes one of them loses the whole company.
const greenhouseEnrichedBoard = `{
	"jobs": [
		{
			"absolute_url": "https://boards.greenhouse.io/acme/jobs/4001",
			"id": 4001,
			"internal_job_id": 900001,
			"title": "Security Engineer",
			"location": {"name": "Remote - US"},
			"updated_at": "2026-05-01T12:00:00-04:00",
			"first_published": "2026-04-20T09:30:00Z",
			"requisition_id": "JR0012345"
		},
		{
			"absolute_url": "https://boards.greenhouse.io/acme/jobs/4002",
			"id": 4002,
			"title": "Detection Analyst",
			"location": {"name": "Austin, TX"},
			"updated_at": "2026-05-02T08:00:00Z",
			"requisition_id": 41815
		},
		{
			"absolute_url": "https://boards.greenhouse.io/acme/jobs/4003",
			"id": 4003,
			"title": "Engineering Manager",
			"location": {"name": ""},
			"updated_at": "",
			"requisition_id": null
		}
	]
}`

// TestGreenhouseDecodesTheFieldsAlreadyOnTheWire covers the enrichment fields.
//
// updated_at, requisition_id and id were commented out in greenhouseJobs, which
// is a record that somebody saw them in a real response and chose not to decode
// them. They are in the plain list response, so this adds no request and no
// measurable bytes across the 647 Greenhouse sources — the widest source of a
// real timestamp in the project.
func TestGreenhouseDecodesTheFieldsAlreadyOnTheWire(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{"boards-api.greenhouse.io": greenhouseEnrichedBoard})

	postings, errs := drain(Greenhouse(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 3, postings)

	engineer, analyst, manager := postings[0], postings[1], postings[2]

	// A non-UTC zone is converted rather than kept, so a Greenhouse timestamp
	// compares with an Ashby or Lever one as an instant.
	test.Eq(t, time.Date(2026, time.May, 1, 16, 0, 0, 0, time.UTC), engineer.UpdatedAt)
	test.Eq(t, "UTC", engineer.UpdatedAt.Location().String())

	test.Eq(t, "JR0012345", engineer.RequisitionID)

	// The board post id, not internal_job_id: it is the one in absolute_url and
	// the key of Greenhouse's per-job endpoint.
	test.Eq(t, "4001", engineer.ExternalID)
	test.Eq(t, internal.PostingSource{Platform: "greenhouse", Key: "acme"}, engineer.Source)

	// first_published is documented on the per-job endpoint and merely picked up
	// when a list happens to carry it.
	test.Eq(t, time.Date(2026, time.April, 20, 9, 30, 0, 0, time.UTC), engineer.PostedAt)

	// A numeric requisition_id is kept as its digits rather than failing the
	// decode for the entire company.
	test.Eq(t, "41815", analyst.RequisitionID)
	test.Eq(t, "4002", analyst.ExternalID)

	// No first_published means no posted date. It is emphatically not defaulted
	// to updated_at: an employer editing a description does not make a
	// nine-month-old req new, and --posted-since would then quietly fill a
	// "posted this week" query with stale postings.
	test.True(t, analyst.PostedAt.IsZero())
	test.False(t, analyst.UpdatedAt.IsZero())

	// null and "" are absences, not values.
	test.Eq(t, "", manager.RequisitionID)
	test.True(t, manager.UpdatedAt.IsZero())
}

// TestGreenhouseDoesNotAskForDescriptions guards a cost decision, not a
// behaviour.
//
// `?content=true` is one URL parameter on this very request, so it would cost no
// extra request, and it would make prose pay-extraction work here as it does on
// Ashby and Lever. It also inflates the response about 13.7x (Databricks 0.7 MB
// to 9.4 MB, Stripe 0.3 MB to 4.0 MB), which is roughly 65 MB to 900 MB over the
// largest platform in the project, into a nightly crawl that already fails to
// finish inside its 75-minute budget. Until an explicit opt-in exists, turning
// it on must fail this test rather than quietly land.
func TestGreenhouseDoesNotAskForDescriptions(t *testing.T) {
	t.Parallel()

	client, transport := fixtureClient(map[string]string{"boards-api.greenhouse.io": greenhouseEnrichedBoard})

	postings, errs := drain(Greenhouse(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 3, postings)
	must.Len(t, 1, transport.requests)

	test.StrNotContains(t, transport.requests[0], "content=true")

	// And no prose-derived pay can appear from a response that carries no prose.
	for _, posting := range postings {
		test.Nil(t, posting.Compensation)
	}
}

// TestGreenhouseScalarTolerates covers the requisition_id shapes one board can
// mix within a single page.
//
// fetchJSON decodes a whole page at once, so a single field with an unexpected
// JSON type takes down every posting for that company: the silently-empty source
// this project treats as its worst failure.
func TestGreenhouseScalarTolerates(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		`"JR0012345"`:      "JR0012345",
		`41815`:            "41815",
		`null`:             "",
		`""`:               "",
		`true`:             "true",
		`{"id": 1}`:        "",
		`["a"]`:            "",
		`"  padded  "`:     "  padded  ",
		`"quoted \"req\""`: `quoted "req"`,
	}

	for raw, want := range tests {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			var got greenhouseScalar

			must.NoError(t, got.UnmarshalJSON([]byte(raw)))
			test.Eq(t, want, string(got))
		})
	}
}

func TestGreenhouseTimestamp(t *testing.T) {
	t.Parallel()

	tests := map[string]time.Time{
		"2026-05-01T12:00:00-04:00": time.Date(2026, time.May, 1, 16, 0, 0, 0, time.UTC),
		"2026-05-02T08:00:00Z":      time.Date(2026, time.May, 2, 8, 0, 0, 0, time.UTC),

		// Unreadable is the zero time, never an error: one odd posting must not
		// cost a board its other postings.
		"":                     {},
		"2026-05-02":           {},
		"Thu, 02 May 2026 UTC": {},
	}

	for raw, want := range tests {
		t.Run(strings.ReplaceAll(raw, " ", "_"), func(t *testing.T) {
			t.Parallel()

			got := greenhouseTimestamp(raw)

			test.Eq(t, want, got)

			if !want.IsZero() {
				test.Eq(t, "UTC", got.Location().String())
			}
		})
	}
}
