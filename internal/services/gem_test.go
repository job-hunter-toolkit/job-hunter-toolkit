package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/companies"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestGem(t *testing.T) {
	testSingle(t, "bluesky", Gem)
}

func TestGem_all(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	testMultipleParallel(t, slices.Values(GemCompanies), Gem)
}

// gemGraphQLTransport answers Gem's GraphQL endpoint the way GraphQL actually
// behaves: a field the query does not select is absent from the response, even
// though the server holds it.
//
// A stub that always sends every field cannot see this adapter's real bug, which
// lived in the query string rather than in the decoding, so this one honours the
// selection set.
type gemGraphQLTransport struct {
	postings []gemStubPosting
	queries  []string
}

// gemStubPosting is one posting the stub can serve.
type gemStubPosting struct {
	id    string
	extID string
	title string

	// The enrichment half of the posting, served only when the query asks for
	// it, for the same reason as the fields above.
	publishedSec   int
	employmentType string
	requisitionID  string
	locationType   string
	locationRemote bool
}

func (g *gemGraphQLTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}

	var request struct {
		Query     string `json:"query"`
		Variables struct {
			BoardID string `json:"boardId"`
		} `json:"variables"`
	}

	if err := json.Unmarshal(body, &request); err != nil {
		return nil, fmt.Errorf("stub could not decode the GraphQL request: %w", err)
	}

	g.queries = append(g.queries, request.Query)

	rendered := make([]string, 0, len(g.postings))

	for _, posting := range g.postings {
		fields := []string{}

		if strings.Contains(request.Query, " id ") {
			fields = append(fields, fmt.Sprintf(`"id":%q`, posting.id))
		}

		if strings.Contains(request.Query, " extId ") {
			fields = append(fields, fmt.Sprintf(`"extId":%q`, posting.extID))
		}

		if strings.Contains(request.Query, " title ") {
			fields = append(fields, fmt.Sprintf(`"title":%q`, posting.title))
		}

		if strings.Contains(request.Query, " firstPublishedTsSec ") {
			fields = append(fields, fmt.Sprintf(`"firstPublishedTsSec":%d`, posting.publishedSec))
		}

		location := `{"name":"Remote","city":"Austin","isoCountry":"US"`
		if strings.Contains(request.Query, " isRemote ") {
			location += fmt.Sprintf(`,"isRemote":%t`, posting.locationRemote)
		}

		fields = append(fields, `"locations":[`+location+`}]`)

		if strings.Contains(request.Query, " job ") {
			fields = append(fields, fmt.Sprintf(
				`"job":{"employmentType":%q,"requisitionId":%q,"locationType":%q,"department":{"name":"Engineering"}}`,
				posting.employmentType, posting.requisitionID, posting.locationType,
			))
		}

		rendered = append(rendered, "{"+strings.Join(fields, ",")+"}")
	}

	response := `{"data":{"oatsExternalJobPostings":{"jobPostings":[` + strings.Join(rendered, ",") + `]}}}`

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(response)),
		Request:    req,
	}, nil
}

// TestDirectEmployersAreRegistered is a regression test.
//
// internal/companies held two working, tested adapters that no crawl had ever
// run. A crawl is assembled from Builtin, every entry of which is added by an
// init in this package, and `grep -rn "internal/companies"` matched nothing
// outside that package's own tests — so Oxide and Uber were dead code in the
// shipped binary. It is the same failure TestBuiltinRegistryIsPopulated guards
// in the large, and the same one that once silently stopped PeopleForce being
// crawled.
func TestDirectEmployersAreRegistered(t *testing.T) {
	t.Parallel()

	registered := make(map[string]Source)

	for _, source := range Builtin {
		if source.Platform == companies.DirectPlatform {
			registered[source.Company] = source
		}
	}

	for _, want := range []string{"oxide", "uber"} {
		source, ok := registered[want]

		must.True(t, ok, must.Sprintf("%q is not in Builtin, so no crawl can reach it", want))
		test.Eq(t, want, source.Key)
		must.NotNil(t, source.Jobs)
	}

	// SourcesMatching is what the CLI narrows a crawl with, so the registration
	// is only real if it survives that path too.
	must.SliceNotEmpty(t, SourcesMatching([]string{"oxide"}))
}

// TestGemBuildsADistinctURLPerPosting is a regression test.
//
// Every Gem posting URL is built from "extId", but the adapter's own GraphQL
// query never selected it, so the field decoded as "" on every posting and each
// board produced one URL: https://jobs.gem.com/<company>/. internal.Dedupe keys
// on the URL, so all 51 Gem sources collapsed to a single posting each,
// measured at 3 postings in and 1 out.
func TestGemBuildsADistinctURLPerPosting(t *testing.T) {
	t.Parallel()

	transport := &gemGraphQLTransport{postings: []gemStubPosting{
		{id: "uuid-1", extID: "abc123", title: "Security Engineer"},
		{id: "uuid-2", extID: "def456", title: "Platform Engineer"},
		{id: "uuid-3", extID: "ghi789", title: "Data Engineer"},
	}}

	client := &http.Client{Transport: transport}

	postings, errs := drain(Gem(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 3, postings)

	test.Eq(t, "https://jobs.gem.com/acme/abc123", postings[0].URL)
	test.Eq(t, "https://jobs.gem.com/acme/def456", postings[1].URL)
	test.Eq(t, "https://jobs.gem.com/acme/ghi789", postings[2].URL)

	// The point of distinct URLs is surviving the crawl's deduplication, so
	// assert against the thing that was actually deleting these postings.
	deduped, errs := drain(internal.Dedupe(Gem(t.Context(), client, "acme")))

	must.SliceEmpty(t, errs)
	must.Len(t, 3, deduped)
}

// TestGemAsksForTheFieldsItAlreadyModels is a regression test on the same defect
// class as the missing extId.
//
// gemJobs has modelled the department, requisition number, employment type,
// workplace type and publication date since the adapter was written, and the
// query never selected any of them. GraphQL returns exactly what is asked for,
// so each decoded as its zero value forever: modelled, read, and always empty.
// They ride in the one POST the adapter already sends, so the whole enrichment
// costs no extra request and no extra round trip.
func TestGemAsksForTheFieldsItAlreadyModels(t *testing.T) {
	t.Parallel()

	transport := &gemGraphQLTransport{postings: []gemStubPosting{
		{
			id: "uuid-1", extID: "abc123", title: "Security Engineer",
			publishedSec: 1780000000, employmentType: "FULL_TIME",
			requisitionID: "JR0012345", locationType: "REMOTE", locationRemote: true,
		},
		{
			id: "uuid-2", extID: "def456", title: "Office Manager",
			employmentType: "PER_DIEM", locationType: "Flexible",
		},
	}}

	postings, errs := drain(Gem(t.Context(), &http.Client{Transport: transport}, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)

	test.Eq(t, "Engineering", postings[0].Department)
	test.Eq(t, "JR0012345", postings[0].RequisitionID)
	test.Eq(t, "abc123", postings[0].ExternalID)
	test.Eq(t, internal.EmploymentTypeFullTime, postings[0].EmploymentType)
	test.Eq(t, internal.WorkplaceTypeRemote, postings[0].WorkplaceType)
	test.Eq(t, internal.PostingSource{Platform: gemPlatform, Key: "acme"}, postings[0].Source)
	test.Eq(t, time.Unix(1780000000, 0).UTC(), postings[0].PostedAt)

	must.NotNil(t, postings[0].Remote)
	test.True(t, *postings[0].Remote)

	// Vocabulary the canonical set does not recognise is left empty rather than
	// guessed at, and epoch zero is "never published" rather than 1970.
	test.Eq(t, internal.EmploymentTypeUnknown, postings[1].EmploymentType)
	test.Eq(t, internal.WorkplaceTypeUnknown, postings[1].WorkplaceType)
	test.True(t, postings[1].PostedAt.IsZero())
	test.Nil(t, postings[1].Remote)
}

// TestGemJoinsOnlyThePartsTheBoardFilledIn covers a location string that used to
// be assembled unconditionally, so a posting with no city read "Remote, , US" —
// a hole for `--location` substring matching to match around.
func TestGemJoinsOnlyThePartsTheBoardFilledIn(t *testing.T) {
	t.Parallel()

	test.Eq(t, "Remote, US", joinNonEmpty(", ", "Remote", "", "US"))
	test.Eq(t, "Austin", joinNonEmpty(", ", "", "Austin", "  "))
	test.Eq(t, "", joinNonEmpty(", ", "", ""))
}

// TestGemFallsBackToTheInternalID covers a tenant that publishes a posting with
// no extId. A link built from the internal id may not resolve, which is
// recoverable; identical URLs are deleted outright by internal.Dedupe and the
// posting is never seen again.
func TestGemFallsBackToTheInternalID(t *testing.T) {
	t.Parallel()

	transport := &gemGraphQLTransport{postings: []gemStubPosting{
		{id: "uuid-1", extID: "", title: "No External ID"},
		{id: "", extID: "", title: "No Identifier At All"},
		{id: "uuid-3", extID: "ghi789", title: "Data Engineer"},
	}}

	postings, errs := drain(Gem(t.Context(), &http.Client{Transport: transport}, "acme"))

	must.SliceEmpty(t, errs)

	// The posting with no identifier of any kind has no reachable link, so it is
	// dropped rather than emitted pointing at the board's index page.
	must.Len(t, 2, postings)

	test.Eq(t, "https://jobs.gem.com/acme/uuid-1", postings[0].URL)
	test.Eq(t, "https://jobs.gem.com/acme/ghi789", postings[1].URL)
}

// TestGemStopsWhenTheConsumerDoes guards the iterator contract the health
// command depends on: it caps each source at 100 postings by returning false
// from yield, and an adapter that keeps going afterwards risks calling yield
// again, which panics.
func TestGemStopsWhenTheConsumerDoes(t *testing.T) {
	t.Parallel()

	transport := &gemGraphQLTransport{postings: []gemStubPosting{
		{id: "uuid-1", extID: "abc123", title: "Security Engineer"},
		{id: "uuid-2", extID: "def456", title: "Platform Engineer"},
		{id: "uuid-3", extID: "ghi789", title: "Data Engineer"},
	}}

	var seen int

	for range Gem(t.Context(), &http.Client{Transport: transport}, "acme") {
		seen++

		break
	}

	test.Eq(t, 1, seen)
}
