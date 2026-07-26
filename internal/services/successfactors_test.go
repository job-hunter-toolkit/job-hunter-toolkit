package services

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// successFactorsFeedFixture is a tenant feed in the shape SAP's RMK runtime
// serves for `?career_ns=job_listing_summary&resultType=XML`.
//
// It is built from the documented response shape rather than captured from a
// live tenant, because this project's containers cannot reach a job board. Three
// things in it are deliberate traps rather than decoration:
//
//   - the empty "<>...</>" tag on an unconfigured facet, which is why this feed
//     cannot be read with encoding/xml at all;
//   - the "[[salaryMin]]" merge token, which the career site's own JavaScript
//     would have substituted and a plain HTTP client receives literally;
//   - "Country - Career" appearing before "Geographic Location", so a reader
//     that takes the first location-ish facet in document order publishes a
//     country where a city was available.
const successFactorsFeedFixture = `<?xml version="1.0" encoding="UTF-8"?>
<Job-Listing>
	<Job>
		<JobTitle><![CDATA[Senior Process Engineer (R&D)]]></JobTitle>
		<ReqId>217384</ReqId>
		<Posted-Date>03/04/2026</Posted-Date>
		<Job-Description><![CDATA[<p>We are hiring an engineer.</p><p>The base salary range for this role is $120,000 - $150,000 per year.</p>]]></Job-Description>
		<filter1><label>Country - Career</label><value>Germany</value></filter1>
		<filter2><label>Geographic Location</label><value><![CDATA[Ludwigshafen, DE]]></value></filter2>
		<mfield1><label>Job Function</label><value>Engineering</value></mfield1>
		<mfield2><label>Employment Type</label><value>Full-Time</value></mfield2>
		<mfield3><label>Location Flexibility</label><value>Hybrid</value></mfield3>
		<><label>Unconfigured</label><value>[[filter7]]</value></>
		<salaryMin>[[salaryMin]]</salaryMin>
	</Job>
	<Job>
		<JobTitle>Werkstudent Logistik</JobTitle>
		<ReqId>217999</ReqId>
		<Posted-Date>07/22/2026</Posted-Date>
		<Job-Description><![CDATA[<p>Wir suchen Verst&auml;rkung.</p>]]></Job-Description>
	</Job>
	<Job>
		<JobTitle>Posting with no requisition id</JobTitle>
		<Posted-Date>07/22/2026</Posted-Date>
	</Job>
</Job-Listing>`

// successFactorsTestTenant is the triple the fixtures above are served for.
const successFactorsTestTenant = "acme,acmeP,career5.successfactors.eu"

func TestSuccessFactorsParsesFeed(t *testing.T) {
	t.Parallel()

	client, transport := fixtureClient(map[string]string{
		"career5.successfactors.eu": successFactorsFeedFixture,
	})

	postings, errs := drain(SuccessFactors(t.Context(), client, successFactorsTestTenant))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)

	// The whole point of this platform: one enterprise's entire open-req corpus
	// for one request, with no detail fan-out and no pagination.
	must.Len(t, 1, transport.requests)
	test.StrContains(t, transport.requests[0], "company=acmeP")
	test.StrContains(t, transport.requests[0], "career_ns=job_listing_summary")
	test.StrContains(t, transport.requests[0], "resultType=XML")

	engineer, student := postings[0], postings[1]

	test.Eq(t, "acme", engineer.Company)
	test.Eq(t, "Senior Process Engineer (R&D)", engineer.Title)

	// CDATA is unwrapped but not re-interpreted: the ampersand in "R&D" is
	// literal text, and entity-decoding it a second time would be wrong.
	test.StrNotContains(t, engineer.Title, "CDATA")

	// The posting URL is synthesized from the requisition id, which is what
	// keeps this platform at one request per employer.
	test.Eq(t,
		"https://career5.successfactors.eu/career?company=acmeP&career_job_req_id=217384&career_ns=job_application",
		engineer.URL,
	)

	// "Geographic Location" beats "Country - Career" even though the country
	// facet comes first in the document: priority belongs to this adapter, not
	// to a tenant's column order.
	test.Eq(t, "Ludwigshafen, DE", engineer.Location)

	test.Eq(t, "Engineering", engineer.Department)
	test.Eq(t, internal.EmploymentTypeFullTime, engineer.EmploymentType)

	// "Location Flexibility" is a workplace picklist, not a place. Reading it as
	// a location would have published "Hybrid" as this job's city.
	test.Eq(t, internal.WorkplaceTypeHybrid, engineer.WorkplaceType)

	test.Eq(t, time.Date(2026, time.March, 4, 0, 0, 0, 0, time.UTC), engineer.PostedAt)
	test.Eq(t, "UTC", engineer.PostedAt.Location().String())

	// RMK publishes a single identifier that serves as both the employer's
	// requisition number and the ATS's posting key.
	test.Eq(t, "217384", engineer.RequisitionID)
	test.Eq(t, "217384", engineer.ExternalID)

	test.Eq(t, internal.PostingSource{Platform: "successfactors", Key: successFactorsTestTenant}, engineer.Source)

	// The description is already on the wire, so a pay range read out of its
	// prose costs no request. It carries description provenance, never employer.
	must.NotNil(t, engineer.Compensation)
	test.Eq(t, internal.ProvenanceDescription, engineer.Compensation.Provenance)
	test.Eq(t, 120000.0, engineer.Compensation.Min)
	test.Eq(t, 150000.0, engineer.Compensation.Max)

	// A tenant that configures no facets still yields a usable posting.
	test.Eq(t, "Werkstudent Logistik", student.Title)
	test.Eq(t, "unknown/remote", student.Location)
	test.Eq(t, "", student.Department)
	test.Eq(t, internal.WorkplaceTypeUnknown, student.WorkplaceType)
	test.Eq(t, time.Date(2026, time.July, 22, 0, 0, 0, 0, time.UTC), student.PostedAt)

	// The third <Job> block has no requisition id, so no URL can be built for
	// it and it is dropped rather than published without a link.
	for _, posting := range postings {
		test.StrHasPrefix(t, "https://", posting.URL)
	}
}

// TestSuccessFactorsStripsMergeTokens guards the RMK-specific trap: the feed is
// a template the career site's JavaScript would have filled in, so a plain HTTP
// client sees "[[salaryMin]]" where a browser sees a number. Publishing one of
// those as a location or a title would put template syntax in front of a job
// seeker.
func TestSuccessFactorsStripsMergeTokens(t *testing.T) {
	t.Parallel()

	const feed = `<Job-Listing><Job>
		<JobTitle><![CDATA[Analyst [[filter3]]]]></JobTitle>
		<ReqId>1</ReqId>
		<filter1><label>Geographic Location</label><value>[[filter1]]</value></filter1>
	</Job></Job-Listing>`

	client, _ := fixtureClient(map[string]string{"career5.successfactors.eu": feed})

	postings, errs := drain(SuccessFactors(t.Context(), client, successFactorsTestTenant))

	must.SliceEmpty(t, errs)
	must.Len(t, 1, postings)

	test.Eq(t, "Analyst", postings[0].Title)

	// A facet whose entire value was a merge token carries no information, so
	// the posting falls back to the same placeholder an absent facet gets.
	test.Eq(t, "unknown/remote", postings[0].Location)
}

// TestSuccessFactorsRejectsANonFeedResponse covers the failure this platform
// makes easiest to hit. companyId is case-sensitive and the host number and TLD
// are both tenant-specific, so a mis-transcribed triple is likely — and the
// wrong host answers HTTP 200 with a small HTML page rather than an error. A
// status-code check would read that as an employer with nothing to offer.
func TestSuccessFactorsRejectsANonFeedResponse(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"career5.successfactors.eu": `<html><body>Career site not found</body></html>`,
	})

	postings, errs := drain(SuccessFactors(t.Context(), client, successFactorsTestTenant))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)

	// The error has to name all three parts of the triple, because any of them
	// can be the one that is wrong.
	must.StrContains(t, errs[0].Error(), "acme")
	must.StrContains(t, errs[0].Error(), "acmeP")
	must.StrContains(t, errs[0].Error(), "career5.successfactors.eu")
}

// TestSuccessFactorsReportsAFeedItCannotRead is the guard that matters most for
// this adapter's honesty. Every element name it looks for came from
// documentation and from other people's implementations, never from a response
// decoded here. If SAP renames <JobTitle> or <ReqId>, the failure mode without
// this check is a tenant that reports success and contributes nothing.
func TestSuccessFactorsReportsAFeedItCannotRead(t *testing.T) {
	t.Parallel()

	const renamed = `<Job-Listing>
		<Job><Job-Title>Renamed</Job-Title><Requisition-Id>1</Requisition-Id></Job>
		<Job><Job-Title>Renamed too</Job-Title><Requisition-Id>2</Requisition-Id></Job>
	</Job-Listing>`

	client, _ := fixtureClient(map[string]string{"career5.successfactors.eu": renamed})

	postings, errs := drain(SuccessFactors(t.Context(), client, successFactorsTestTenant))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "acme")
	must.StrContains(t, errs[0].Error(), "layout may have changed")
}

// TestSuccessFactorsAcceptsAnEmptyFeed is the other half of that rule: a feed
// that is unmistakably this feed and lists nothing is an employer with no open
// reqs, which is unusual but not an error.
func TestSuccessFactorsAcceptsAnEmptyFeed(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"career5.successfactors.eu": `<?xml version="1.0"?><Job-Listing></Job-Listing>`,
	})

	postings, errs := drain(SuccessFactors(t.Context(), client, successFactorsTestTenant))

	test.SliceEmpty(t, postings)
	test.SliceEmpty(t, errs)
}

func TestSuccessFactorsReportsANon200(t *testing.T) {
	t.Parallel()

	transport := &fixtureTransport{
		routes: map[string]string{"career5.successfactors.eu": "gone"},
		status: http.StatusServiceUnavailable,
	}

	postings, errs := drain(SuccessFactors(t.Context(), &http.Client{Transport: transport}, successFactorsTestTenant))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "acme")
}

// endlessTransport answers with a body that never ends, which is what a hung
// proxy or a mis-routed streaming endpoint looks like to a client that has to
// hold a whole document to read it.
type endlessTransport struct{}

func (endlessTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{},
		Body:       io.NopCloser(endlessReader{}),
		Request:    req,
	}, nil
}

// endlessReader yields the same feed fragment forever.
type endlessReader struct{}

func (endlessReader) Read(p []byte) (int, error) {
	const chunk = "<Job-Listing><Job><JobTitle>x</JobTitle><ReqId>1</ReqId></Job>"

	var written int

	for written < len(p) {
		written += copy(p[written:], chunk)
	}

	return written, nil
}

// TestSuccessFactorsRefusesAnEndlessFeed covers this adapter's equivalent of a
// pagination ceiling. It makes one request and holds the whole response, so
// without a bound a tenant that never stops sending would consume a worker's
// memory for as long as the crawl runs. Truncating silently would be worse than
// failing: it would drop the tail of an employer's postings with no sign.
func TestSuccessFactorsRefusesAnEndlessFeed(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: endlessTransport{}}

	postings, errs := drain(SuccessFactors(t.Context(), client, successFactorsTestTenant))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "acme")
	must.StrContains(t, errs[0].Error(), "refusing to read more than")
}

func TestSuccessFactorsRejectsAMalformedTenant(t *testing.T) {
	t.Parallel()

	badKeys := []string{
		"acme",
		"acme,acmeP",
		"acme,acmeP,career5.successfactors.eu,extra",
		"acme,,career5.successfactors.eu",
		",acmeP,career5.successfactors.eu",
	}

	for _, key := range badKeys {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			// The transport is one that answers everything, so the only way this
			// can produce an error is by refusing to build a request at all.
			client, transport := fixtureClient(map[string]string{"successfactors": successFactorsFeedFixture})

			postings, errs := drain(SuccessFactors(t.Context(), client, key))

			test.SliceEmpty(t, postings)
			test.SliceEmpty(t, transport.requests)
			must.Len(t, 1, errs)
			must.StrContains(t, errs[0].Error(), "invalid SuccessFactors tenant")

			// A malformed entry keeps its raw text as a display name, so it can
			// be traced back to the line that produced it.
			test.Eq(t, key, successFactorsCompanyName(key))
		})
	}
}

// TestSuccessFactorsResolvesSlashDatesAcrossTheFeed covers the one date decision
// this adapter makes that [phenomPostedAt] refuses to make at all.
//
// 03/04/2026 is two different days depending on who wrote it. RMK documents
// MM/DD/YYYY, and a feed carries every open req at once, so a single value whose
// first component exceeds 12 proves the tenant writes days first and settles the
// reading for the whole feed.
func TestSuccessFactorsResolvesSlashDatesAcrossTheFeed(t *testing.T) {
	t.Parallel()

	feed := func(dates ...string) string {
		var b strings.Builder

		b.WriteString("<Job-Listing>")

		for i, date := range dates {
			fmt.Fprintf(&b, "<Job><JobTitle>Job %d</JobTitle><ReqId>%d</ReqId><Posted-Date>%s</Posted-Date></Job>", i, i, date)
		}

		b.WriteString("</Job-Listing>")

		return b.String()
	}

	postedAt := func(t *testing.T, dates ...string) []time.Time {
		t.Helper()

		client, _ := fixtureClient(map[string]string{"career5.successfactors.eu": feed(dates...)})

		postings, errs := drain(SuccessFactors(t.Context(), client, successFactorsTestTenant))

		must.SliceEmpty(t, errs)
		must.Len(t, len(dates), postings)

		stamps := make([]time.Time, 0, len(postings))
		for _, posting := range postings {
			stamps = append(stamps, posting.PostedAt)
		}

		return stamps
	}

	t.Run("documented month-first reading", func(t *testing.T) {
		t.Parallel()

		got := postedAt(t, "03/04/2026", "07/09/2026")

		test.Eq(t, time.Date(2026, time.March, 4, 0, 0, 0, 0, time.UTC), got[0])
		test.Eq(t, time.Date(2026, time.July, 9, 0, 0, 0, 0, time.UTC), got[1])
	})

	t.Run("one impossible month flips the whole feed", func(t *testing.T) {
		t.Parallel()

		// 22 cannot be a month, so this tenant writes days first, and the
		// ambiguous 03/04 in the same feed has to be read the same way.
		got := postedAt(t, "03/04/2026", "22/07/2026")

		test.Eq(t, time.Date(2026, time.April, 3, 0, 0, 0, 0, time.UTC), got[0])
		test.Eq(t, time.Date(2026, time.July, 22, 0, 0, 0, 0, time.UTC), got[1])
	})

	t.Run("ISO dates are read regardless", func(t *testing.T) {
		t.Parallel()

		got := postedAt(t, "2026-05-06", "2026-05-07T08:09:10Z")

		test.Eq(t, time.Date(2026, time.May, 6, 0, 0, 0, 0, time.UTC), got[0])
		test.Eq(t, time.Date(2026, time.May, 7, 8, 9, 10, 0, time.UTC), got[1])
	})

	t.Run("an unreadable date is absent, not the epoch", func(t *testing.T) {
		t.Parallel()

		got := postedAt(t, "sometime last spring")

		test.True(t, got[0].IsZero())
	})
}

// candidateTenants reads a staged candidate list from testdata, dropping
// comments and blank lines.
func candidateTenants(t *testing.T, name string) map[string]bool {
	t.Helper()

	file, err := os.Open(filepath.Join("testdata", "candidates", name))
	must.NoError(t, err)

	defer file.Close()

	entries := make(map[string]bool)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line, _, _ := strings.Cut(scanner.Text(), "#")

		if line = strings.TrimSpace(line); line != "" {
			entries[line] = true
		}
	}

	must.NoError(t, scanner.Err())

	return entries
}

// companiesOnOtherPlatforms returns the display name of every registered source
// that is not on the given platform.
func companiesOnOtherPlatforms(platform string) map[string]string {
	companies := make(map[string]string, len(Builtin))

	for _, source := range Builtin {
		if source.Platform == platform {
			continue
		}

		companies[strings.ToLower(source.Company)] = source.Platform
	}

	return companies
}

// TestSuccessFactorsAddsNoDoubleCountedEmployer is the guard for the one hazard
// these two new enterprise lanes bring that no existing check would catch.
//
// [internal.Dedupe] keys on URL, and the same job has a different URL on each
// platform it is published to, so an employer crawled twice contributes its
// postings twice. jobs_record.txt is a trend line across runs, and a step change
// that reflects no hiring is indistinguishable from one that does. This caught a
// real overlap while these lists were being assembled: Marriott is on Oracle
// Recruiting Cloud and was already registered on Jibe.
func TestSuccessFactorsAddsNoDoubleCountedEmployer(t *testing.T) {
	t.Parallel()

	elsewhere := companiesOnOtherPlatforms(successFactorsPlatform)

	for _, key := range SuccessFactorsTenants {
		company := successFactorsCompanyName(key)

		platform, clash := elsewhere[strings.ToLower(company)]

		test.False(t, clash, test.Sprintf("company %q is registered on both successfactors and %s, so its postings would be counted twice; pick one route", company, platform))
	}
}

// TestSuccessFactorsTenantsComeFromTheCandidateFile keeps the registered list
// honest about its own provenance.
//
// The registered list is a hand-picked staging subset of a much larger candidate
// file that nobody in this project has been able to probe, and the whole point
// of that arrangement is that promoting an entry is a deliberate act. A triple
// here that is not in the candidate file was either typed from memory or edited
// after the fact, and a single wrong character in a case-sensitive companyId
// produces a source that fails every night for a reason nobody can see from the
// diff.
func TestSuccessFactorsTenantsComeFromTheCandidateFile(t *testing.T) {
	t.Parallel()

	candidates := candidateTenants(t, "successfactors_tenants.txt")

	must.Greater(t, 100, len(candidates), must.Sprint("the candidate file should hold the full researched list"))

	seen := make(map[string]bool, len(SuccessFactorsTenants))

	for _, key := range SuccessFactorsTenants {
		tenant, err := parseSuccessFactorsTenant(key)
		must.NoError(t, err, must.Sprintf("registered tenant %q", key))

		test.False(t, seen[tenant.slug], test.Sprintf("company %q is registered twice", tenant.slug))
		seen[tenant.slug] = true

		test.True(t, candidates[key], test.Sprintf("registered tenant %q is not in testdata/candidates/successfactors_tenants.txt", key))
	}

	// Staging is the point: registering the whole candidate list would put
	// hundreds of unprobed tenants into a crawl that already misses its
	// deadline, and enough failing ones would trip the source-health alarm that
	// is supposed to mean a real platform broke.
	test.Less(t, len(candidates), len(SuccessFactorsTenants), test.Sprint("the registered list should stay a subset of the candidates"))
}
