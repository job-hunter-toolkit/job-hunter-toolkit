package services

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestPersonio(t *testing.T) {
	testSingle(t, "personio", Personio)
}

func TestPersonio_all(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	testMultipleParallel(t, slices.Values(PersonioCompanies), Personio)
}

// TestPersonioParsesFeed covers the split this platform makes that no other
// board here does: tenure ("permanent", "intern") and hours ("full-time",
// "part-time") are two separate elements, and one employment type has to be
// resolved from both.
func TestPersonioParsesFeed(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"acme.jobs.personio.de/xml": `<?xml version="1.0" encoding="utf-8"?>
		<workzag-jobs>
			<position>
				<id>1234567</id>
				<name>  Security Engineer  </name>
				<office>Munich</office>
				<additionalOffices>
					<office>Berlin</office>
					<office>Munich</office>
				</additionalOffices>
				<department>Engineering</department>
				<recruitingCategory>Tech</recruitingCategory>
				<employmentType>permanent</employmentType>
				<schedule>full-time</schedule>
				<seniority>experienced</seniority>
				<createdAt>2026-04-30T16:21:55+02:00</createdAt>
				<jobDescriptions>
					<jobDescription><name>Your mission</name><value>&lt;p&gt;Ship things&lt;/p&gt;</value></jobDescription>
				</jobDescriptions>
			</position>
			<position>
				<id>7654321</id>
				<name>Working Student Marketing</name>
				<office>Remote</office>
				<department>Marketing</department>
				<recruitingCategory>Marketing</recruitingCategory>
				<employmentType>intern</employmentType>
				<schedule>part-time</schedule>
				<seniority>student</seniority>
				<createdAt>2026-05-04T08:00:00+00:00</createdAt>
			</position>
			<position>
				<id>2468101</id>
				<name>Support Agent</name>
				<office>Vienna</office>
				<department>Operations</department>
				<employmentType>permanent</employmentType>
				<schedule>full-or-part-time</schedule>
			</position>
			<position>
				<id></id>
				<name>Draft with no id</name>
				<office>Munich</office>
			</position>
		</workzag-jobs>`,
	})

	postings, errs := drain(Personio(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 3, postings)

	engineer := postings[0]

	test.Eq(t, "Security Engineer", engineer.Title)
	test.Eq(t, "acme", engineer.Company)

	// The feed carries no link of its own, so the public posting page is built
	// from the tenant host and the position id.
	test.Eq(t, "https://acme.jobs.personio.de/job/1234567", engineer.URL)

	// Offices are deduplicated: Munich is both the primary and an additional one.
	test.Eq(t, "Munich; Berlin", engineer.Location)
	test.Eq(t, "Engineering", engineer.Department)
	test.Eq(t, "Tech", engineer.Team)
	test.Eq(t, "experienced", engineer.Seniority)
	test.Eq(t, "1234567", engineer.ExternalID)
	test.Eq(t, internal.PostingSource{Platform: "personio", Key: "acme"}, engineer.Source)

	// "permanent" is deliberately unrecognised — a permanent part-time role is
	// ordinary — so the hours element is what answers.
	test.Eq(t, internal.EmploymentTypeFullTime, engineer.EmploymentType)

	test.Eq(t, "2026-04-30T14:21:55Z", engineer.PostedAt.Format(time.RFC3339))

	// Personio publishes no structured workplace field, so the location text is
	// left for IsRemote's heuristic rather than being promoted to an answer.
	test.Eq(t, internal.WorkplaceTypeUnknown, engineer.WorkplaceType)
	test.Nil(t, engineer.Remote)

	student := postings[1]

	// The tenure element wins when it says something specific: an internship
	// that is also part-time is better filed as an internship.
	test.Eq(t, internal.EmploymentTypeInternship, student.EmploymentType)
	test.Eq(t, "Remote", student.Location)

	// The recruiting category repeats the department here, so it is not also
	// published as a team.
	test.Eq(t, "Marketing", student.Department)
	test.Eq(t, "", student.Team)

	support := postings[2]

	// "full-or-part-time" squashes to a string ending in "parttime", so the
	// normalizer would read a role open to either as part-time. A filter cannot
	// tell a wrong answer from a right one, so it stays empty.
	test.Eq(t, internal.EmploymentTypeUnknown, support.EmploymentType)
	test.True(t, support.PostedAt.IsZero())
}

// personioFixture reads a feed captured from a live Personio career site.
//
// The capture under testdata is what the board answered with each position's
// <jobDescriptions> block removed — the entire posting body in entity-encoded
// HTML, which this adapter deliberately does not decode. Every other element,
// and every value, is the board's own.
func personioFixture(t *testing.T, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", name))
	must.NoError(t, err)

	return string(body)
}

// TestPersonioParsesACapturedLiveFeed is the fixture that decides whether this
// adapter reads Personio, as opposed to reading the shape a document said
// Personio has. The body is https://rtc-rath-gmbh.jobs.personio.de/xml?language=en
// as captured on 2026-07-28.
//
// What the capture establishes, and what the hand-written fixture above cannot:
//
//   - <salaryInformation> exists. docs/research/ats-platform-survey.md does not
//     mention it and this adapter shipped publishing no pay for Personio at all.
//     It carries min, an optional max, a currencySymbol, an ISO currencyCode and
//     a "type", and appears on 1,192 of the 11,938 live positions measured
//     across the platform — 981 with both bounds and 211, like the two here,
//     with only a minimum.
//   - the bounds are decimal STRINGS ("18.50"), not XML numbers.
//   - "type" is adverbial: "hourly", "monthly", "yearly". An hourly rate of
//     18.50 would have been inferred correctly by magnitude, but 334 live
//     positions are monthly and every one of those would have been republished
//     as an annual salary.
//   - <office>Remote</office> is a real live value, and it stays location text.
//     [internal.NormalizeWorkplaceType] must not be fed it, which is why this
//     platform leaves WorkplaceType unknown and Remote nil and lets
//     [internal.JobPosting.IsRemote]'s heuristic run on the location string.
//   - the feed also carries subcompany, keywords, yearsOfExperience, occupation
//     and occupationCategory, none of which [internal.JobPosting] has a home for.
func TestPersonioParsesACapturedLiveFeed(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"rtc-rath-gmbh.jobs.personio.de/xml": personioFixture(t, "personio_rtc_rath_positions.xml"),
	})

	postings, errs := drain(Personio(t.Context(), client, "rtc-rath-gmbh"))

	must.SliceEmpty(t, errs)
	must.Len(t, 4, postings)

	lead := postings[0]

	test.Eq(t, "rtc-rath-gmbh", lead.Company)
	test.Eq(t, "(Senior) Projektsteuerer Bahnbau (m/w/d)", lead.Title)
	test.Eq(t, "https://rtc-rath-gmbh.jobs.personio.de/job/2483204", lead.URL)
	test.Eq(t, "Project Engineering", lead.Department)
	test.Eq(t, "Festangestellte (Projekte)", lead.Team)
	test.Eq(t, "experienced", lead.Seniority)
	test.Eq(t, "2026-01-11T20:04:28Z", lead.PostedAt.Format(time.RFC3339))

	// "permanent" is not an engagement, so the schedule decides.
	test.Eq(t, internal.EmploymentTypeFullTime, lead.EmploymentType)

	// An office literally named "Remote" is location text and nothing more.
	test.Eq(t, "Remote", lead.Location)
	test.Eq(t, internal.WorkplaceTypeUnknown, lead.WorkplaceType)
	test.Nil(t, lead.Remote)

	// Most positions publish no pay, and that must stay nil rather than becoming
	// an empty range.
	test.Nil(t, lead.Compensation)

	helper := postings[1]

	// additionalOffices are joined onto the primary one.
	test.Eq(t, "Koblenz; Frankfurt am Main; Bonn", helper.Location)
	test.Eq(t, internal.EmploymentTypeTemporary, helper.EmploymentType)

	must.NotNil(t, helper.Compensation)
	test.Eq(t, 18.50, helper.Compensation.Min)
	test.Eq(t, 0.0, helper.Compensation.Max)
	test.Eq(t, "EUR", helper.Compensation.Currency)
	test.Eq(t, internal.PeriodHour, helper.Compensation.Period)
	test.Eq(t, internal.ProvenanceEmployer, helper.Compensation.Provenance)
}

// personioRedirectTransport answers every request with a 200 that was served
// from a different URL, which is what [http.Client] leaves behind on
// Response.Request after following a redirect.
type personioRedirectTransport struct {
	to string
}

func (p personioRedirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	final := req.Clone(req.Context())

	target, err := url.Parse(p.to)
	if err != nil {
		return nil, err
	}

	final.URL = target

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{"Content-Type": []string{"text/html"}},
		Body:       io.NopCloser(strings.NewReader("<html><body>Personio</body></html>")),
		Request:    final,
	}, nil
}

// TestPersonioReportsARedirectOffTheTenantHost pins the shape a dead Personio
// tenant actually has. It does not 404: it answers HTTP 307 to
// https://personio.com, on .de and .com alike, and the shared client follows
// redirects — so without this check the crawl fetches the vendor's marketing
// page, on a host internal/httpx has no rate-limit policy for. Six of the 999
// candidates probed on 2026-07-28 were dead this way, and probing only those six
// was enough to make personio.com start answering 429.
func TestPersonioReportsARedirectOffTheTenantHost(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: personioRedirectTransport{to: "https://personio.com/"}}

	postings, errs := drain(Personio(t.Context(), client, "gone"))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "gone")
	must.StrContains(t, errs[0].Error(), "does not publish a feed")
}

// TestPersonioToleratesMarkupThatIsNotStrictXML is a regression guard for the
// failure successfactors.go documents in another costume: this feed is XML
// carrying HTML written by whoever typed the job ad, and neither an HTML entity
// nor a bare ampersand is valid XML. Either one is enough for a strict parser to
// reject the whole document, costing an entire employer's postings over a
// character in a description this adapter does not even read.
//
// Both halves are load-bearing and they fix different things: the HTML entity
// table is what keeps "&nbsp;" from reaching a job seeker as those six literal
// characters in a title, and non-strict parsing is what keeps "R&D" from being
// an error at all.
func TestPersonioToleratesMarkupThatIsNotStrictXML(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"personio.de": `<?xml version="1.0" encoding="utf-8"?>
		<workzag-jobs>
			<position>
				<id>1</id>
				<name>Security&nbsp;Engineer</name>
				<office>Munich</office>
				<jobDescriptions>
					<jobDescription><name>Mission</name><value>Ship&nbsp;things</value></jobDescription>
				</jobDescriptions>
			</position>
			<position>
				<id>2</id>
				<name>R&D Engineer</name>
				<office>Munich</office>
			</position>
		</workzag-jobs>`,
	})

	postings, errs := drain(Personio(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 2, postings)

	// The entity became one non-breaking space rather than the six characters
	// "&nbsp;", which is what a job seeker would otherwise have read.
	test.Eq(t, "Security\u00a0Engineer", postings[0].Title)
	test.Eq(t, "R&D Engineer", postings[1].Title)
}

// TestPersonioReportsANonFeedDocument covers the platform's own failure mode:
// publishing the feed is a per-tenant switch, so a tenant that never enabled it,
// or a subdomain that has been reclaimed, answers 200 with a page instead. A
// parser that shrugged that off would report a live company as one that is not
// hiring.
func TestPersonioReportsANonFeedDocument(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"personio.de": `<!DOCTYPE html><html><body><h1>Careers</h1></body></html>`,
	})

	postings, errs := drain(Personio(t.Context(), client, "acme"))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "acme")
}

// TestPersonioReportsTruncatedFeed covers a body that stops mid-document: it
// fails loudly rather than decoding into the positions that did arrive, because
// half a company's postings reported as all of them is the silent failure this
// project refuses to produce.
func TestPersonioReportsTruncatedFeed(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"personio.de": `<workzag-jobs><position><id>1</id><name>Security Engineer</name>`,
	})

	postings, errs := drain(Personio(t.Context(), client, "acme"))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "acme")
}

func TestPersonioReportsHTTPError(t *testing.T) {
	t.Parallel()

	transport := &fixtureTransport{
		routes: map[string]string{"personio.de": `<workzag-jobs></workzag-jobs>`},
		status: http.StatusNotFound,
	}

	postings, errs := drain(Personio(t.Context(), &http.Client{Transport: transport}, "gone"))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "gone")
}

// TestPersonioReportsEmptyBoardWithoutError: a feed with no positions is a real
// answer from a company that is not hiring today, and docs/adding-a-source.md is
// explicit that is not a failure.
func TestPersonioReportsEmptyBoardWithoutError(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{"personio.de": `<workzag-jobs></workzag-jobs>`})

	postings, errs := drain(Personio(t.Context(), client, "acme"))

	test.SliceEmpty(t, postings)
	test.SliceEmpty(t, errs)
}

// TestPersonioReportsPositionsThatYieldNoPostings covers a renamed element: the
// feed is full of positions and not one produces a posting, which no live board
// does.
func TestPersonioReportsPositionsThatYieldNoPostings(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"personio.de": `<workzag-jobs>
			<position><positionId>1</positionId><jobTitle>Security Engineer</jobTitle></position>
			<position><positionId>2</positionId><jobTitle>Support Agent</jobTitle></position>
		</workzag-jobs>`,
	})

	postings, errs := drain(Personio(t.Context(), client, "acme"))

	test.SliceEmpty(t, postings)
	must.Len(t, 1, errs)
	must.StrContains(t, errs[0].Error(), "acme")
	must.StrContains(t, errs[0].Error(), "2 positions decoded")
}

// TestPersonioUsesTheRegisteredHost covers the .com tenants: a key containing a
// dot is a full host, so both the feed request and the posting links follow it
// without this adapter learning a per-tenant domain table.
func TestPersonioUsesTheRegisteredHost(t *testing.T) {
	t.Parallel()

	client, transport := fixtureClient(map[string]string{
		"acme.jobs.personio.com/xml": `<workzag-jobs>
			<position><id>42</id><name>Security Engineer</name><office>Munich</office></position>
		</workzag-jobs>`,
	})

	postings, errs := drain(Personio(t.Context(), client, "acme.jobs.personio.com"))

	must.SliceEmpty(t, errs)
	must.Len(t, 1, postings)

	test.Eq(t, "https://acme.jobs.personio.com/job/42", postings[0].URL)
	test.Eq(t, "acme", postings[0].Company)
	test.Eq(t, "acme.jobs.personio.com", postings[0].Source.Key)

	must.Len(t, 1, transport.requests)
	test.StrContains(t, transport.requests[0], "https://acme.jobs.personio.com/xml")
}

// TestPersonioHost covers key forms this adapter accepts.
func TestPersonioHost(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		key     string
		host    string
		company string
	}{
		{"holidu", "holidu.jobs.personio.de", "holidu"},
		{"  holidu  ", "holidu.jobs.personio.de", "holidu"},
		{"moebel-de", "moebel-de.jobs.personio.de", "moebel-de"},
		{"acme.jobs.personio.com", "acme.jobs.personio.com", "acme"},
		{"https://acme.jobs.personio.com/xml", "acme.jobs.personio.com", "acme"},
	} {
		test.Eq(t, tc.host, personioHost(tc.key), test.Sprintf("key %q", tc.key))
		test.Eq(t, tc.company, personioCompanyName(tc.key), test.Sprintf("key %q", tc.key))
	}
}

// TestPersonioCompaniesComeFromTheCandidateFile keeps the registered list
// honest: every slug in it is one a research pass actually recorded, and the
// registered set stays a small staged subset rather than the whole unprobed
// harvest.
func TestPersonioCompaniesComeFromTheCandidateFile(t *testing.T) {
	t.Parallel()

	candidates := candidateSlugs(t, "personio_slugs.txt")

	must.Greater(t, 100, len(candidates), must.Sprint("the candidate file should hold the full researched list"))

	seen := make(map[string]bool, len(PersonioCompanies))

	for _, slug := range PersonioCompanies {
		test.False(t, seen[slug], test.Sprintf("company %q is registered twice", slug))
		seen[slug] = true

		test.True(t, candidates[slug], test.Sprintf("registered company %q is not in testdata/candidates/personio_slugs.txt", slug))
	}

	test.Less(t, len(candidates), len(PersonioCompanies), test.Sprint("the registered list should stay a subset of the candidates"))
}
