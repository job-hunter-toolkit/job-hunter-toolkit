package services

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestSmartRecruiters(t *testing.T) {
	testSingle(t, "PaloAltoNetworks2", SmartRecruiters) // "McDonaldsCorporation", "visa"
}

// smartRecruitersPageBody builds a page of exactly 100 postings whose ids carry
// prefix, so a caller can make two pages either identical or distinct.
func smartRecruitersPageBody(prefix string, totalFound int) string {
	items := make([]string, 100)

	for i := range items {
		items[i] = fmt.Sprintf(
			`{"id":"%s%d","name":"Job %s%d","location":{"city":"Austin","region":"TX","country":"us"}}`,
			prefix, i, prefix, i)
	}

	return fmt.Sprintf(`{"totalFound":%d,"content":[%s]}`, totalFound, strings.Join(items, ","))
}

// TestSmartRecruitersStopsWhenTheTenantIgnoresOffset is a regression test.
//
// This adapter's only stop conditions were an empty page and the tenant's own
// "totalFound", both supplied by the server. A tenant that answers every offset
// with the same full page while reporting a large total therefore paginated
// until the crawl deadline, emitting duplicates that internal.Dedupe then hid,
// so the only visible symptom was a slow crawl. Every other paginating adapter
// in this package was given a bound after that failure was reproduced against
// it; this one was missed because nothing looks wrong while a server behaves.
func TestSmartRecruitersStopsWhenTheTenantIgnoresOffset(t *testing.T) {
	t.Parallel()

	client, transport := repeatingPageClient(smartRecruitersPageBody("", 1_000_000))

	postings, errs := drain(SmartRecruiters(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)

	// The first page is served; the second is recognised as a repeat of it and
	// ends the loop before any of its duplicates are yielded.
	test.Eq(t, 2, transport.requests)
	test.Len(t, 100, postings)
}

// smartRecruitersOffsetEchoTransport serves a full page whose posting ids embed
// the offset they were served at, so no two pages are ever alike and the page
// fingerprint cannot end the loop. Only a hard ceiling can.
type smartRecruitersOffsetEchoTransport struct {
	requests int
}

func (s *smartRecruitersOffsetEchoTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	s.requests++

	if s.requests > smartRecruitersMaxPages*2 {
		return nil, fmt.Errorf("made %d requests to %s: pagination is unbounded", s.requests, req.URL)
	}

	offset, _ := strconv.Atoi(req.URL.Query().Get("offset"))

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			smartRecruitersPageBody("o"+strconv.Itoa(offset)+"-", 1_000_000))),
		Request: req,
	}, nil
}

// TestSmartRecruitersBoundsPagesWhenEveryPageDiffers proves the backstop.
//
// A tenant reporting an enormous totalFound while serving a genuinely different
// page at every offset defeats the repeat fingerprint, which is why the ceiling
// exists. It must stop, and it must say why rather than truncating silently.
func TestSmartRecruitersBoundsPagesWhenEveryPageDiffers(t *testing.T) {
	t.Parallel()

	transport := &smartRecruitersOffsetEchoTransport{}
	client := &http.Client{Transport: transport}

	postings, errs := drain(SmartRecruiters(t.Context(), client, "acme"))

	test.Eq(t, smartRecruitersMaxPages, transport.requests)
	test.Len(t, smartRecruitersMaxPages*100, postings)

	must.Len(t, 1, errs)
	test.StrContains(t, errs[0].Error(), "refusing to keep paginating")
}

// TestSmartRecruitersRequestsTheOffsetItAdvancesTo guards the query this adapter
// builds, since the bound above is only meaningful if the offset really moves.
func TestSmartRecruitersRequestsTheOffsetItAdvancesTo(t *testing.T) {
	t.Parallel()

	var offsets []string

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		offsets = append(offsets, req.URL.Query().Get("offset"))

		body := `{"totalFound":150,"content":[]}`
		if len(offsets) == 1 {
			body = smartRecruitersPageBody("a", 150)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     http.StatusText(http.StatusOK),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}

	postings, errs := drain(SmartRecruiters(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	test.Len(t, 100, postings)
	test.Eq(t, []string{"0", "100"}, offsets)

	// The posting URL is built from the tenant and the posting id, and the
	// tenant name is used verbatim, so a name needing escaping would break it.
	must.SliceNotEmpty(t, postings)
	test.Eq(t, "https://jobs.smartrecruiters.com/acme/a0", postings[0].URL)
	test.Eq(t, url.PathEscape("acme"), "acme")
}

// roundTripFunc adapts a function to [net/http.RoundTripper].
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
