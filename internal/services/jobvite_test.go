package services

import (
	"slices"
	"strings"
	"testing"
)

func TestJobvite(t *testing.T) {
	testSingle(t, "splunk-careers", Jobvite)
}

func TestJobvite_all(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	testMultipleParallel(t, slices.Values(JobviteCompanies), Jobvite)
}

func TestJobviteStartsAtPageZero(t *testing.T) {
	t.Parallel()

	client, transport := fixtureClient(map[string]string{
		"p=0": `<table><tr>` +
			`<td><a href="/acme/job/abc">Platform Engineer</a></td>` +
			`<td></td><td></td><td>Detroit, Michigan</td>` +
			`</tr></table>`,
		"p=1": `<p ng-non-bindable>No more results</p>`,
	})

	postings, errs := drain(Jobvite(t.Context(), client, "acme"))

	if len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}

	if len(postings) != 1 {
		t.Fatalf("got %d postings, want 1", len(postings))
	}

	if len(transport.requests) != 2 {
		t.Fatalf("made %d requests, want page zero and the empty page one", len(transport.requests))
	}

	if !strings.Contains(transport.requests[0], "p=0") {
		t.Errorf("first request = %q, want page zero", transport.requests[0])
	}
}
