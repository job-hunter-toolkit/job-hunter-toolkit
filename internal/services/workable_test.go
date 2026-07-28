package services

import (
	"strings"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestWorkable(t *testing.T) {
	testSingle(t, "trailofbits", Workable)
}

func TestWorkableUsesWidgetFeed(t *testing.T) {
	t.Parallel()

	client, transport := fixtureClient(map[string]string{
		"api/v1/widget/accounts/acme": `{
			"name": "Acme",
			"jobs": [{
				"title": "  Platform Engineer  ",
				"shortcode": "ABC123",
				"telecommuting": true,
				"url": "https://apply.workable.com/j/ABC123",
				"locations": [{
					"city": "Detroit",
					"region": "Michigan",
					"country": "United States",
					"countryCode": "US",
					"hidden": false
				}]
			}]
		}`,
	})

	postings, errs := drain(Workable(t.Context(), client, "acme"))

	if len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}

	if len(postings) != 1 {
		t.Fatalf("got %d postings, want 1", len(postings))
	}

	if got, want := postings[0].Title, "Platform Engineer"; got != want {
		t.Errorf("Title = %q, want %q", got, want)
	}

	if got, want := postings[0].Location, "Detroit, Michigan, United States; Remote"; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}

	if len(transport.requests) != 1 {
		t.Fatalf("made %d requests, want 1", len(transport.requests))
	}

	if got := transport.requests[0]; !strings.Contains(got, "/api/v1/widget/accounts/acme") {
		t.Errorf("request URL = %q, want the unthrottled widget feed", got)
	}
}

// TestWorkableCarriesTheStructuredRemoteFlag is a regression test.
//
// "telecommuting" is Workable's own remote flag and has been decoded since this
// adapter was written, but its only use was appending the word "Remote" to a
// location string. That left `--remote` to rediscover the flag by searching free
// text — the heuristic that exists for boards which publish nothing, applied to
// a board that publishes an answer.
func TestWorkableCarriesTheStructuredRemoteFlag(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"api/v1/widget/accounts/acme": `{
			"name": "Acme",
			"jobs": [
				{
					"title": "Platform Engineer",
					"shortcode": "ABC123",
					"telecommuting": true,
					"url": "https://apply.workable.com/j/ABC123",
					"locations": [{"city": "Detroit", "region": "Michigan",
					               "country": "United States", "countryCode": "US"}]
				},
				{
					"title": "Office Manager",
					"shortcode": "DEF456",
					"telecommuting": false,
					"url": "https://apply.workable.com/j/DEF456",
					"locations": [{"city": "Detroit", "region": "Michigan",
					               "country": "United States", "countryCode": "US"}]
				}
			]
		}`,
	})

	postings, errs := drain(Workable(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)

	must.NotNil(t, postings[0].Remote)
	test.True(t, *postings[0].Remote)
	test.Eq(t, internal.WorkplaceTypeRemote, postings[0].WorkplaceType)
	test.Eq(t, "ABC123", postings[0].ExternalID)
	test.Eq(t, internal.PostingSource{Platform: workablePlatform, Key: "acme"}, postings[0].Source)

	// telecommuting=false says "not fully remote" and nothing more. Recording it
	// as onsite would invent an office requirement, and recording Remote=false
	// would switch off the location-text fallback for the whole platform.
	test.Nil(t, postings[1].Remote)
	test.Eq(t, internal.WorkplaceTypeUnknown, postings[1].WorkplaceType)
	test.Eq(t, "DEF456", postings[1].ExternalID)
}
