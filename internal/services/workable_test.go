package services

import (
	"strings"
	"testing"
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
