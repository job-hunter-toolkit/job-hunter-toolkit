package services

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// eightfoldPlatform is the platform name this file registers under, shared with
// the [internal.PostingSource] every posting carries so the two cannot drift
// apart.
const eightfoldPlatform = "eightfold"

func init() {
	registerBuiltin(eightfoldPlatform, multiJobsFunc(Eightfold, EightfoldCompanies))
}

// eightfoldPageSize is the number of postings requested per page.
//
// The server hard-caps a page at ten regardless of what "num" asks for: num=20,
// num=25 and num=50 all answer with exactly ten positions. Sending the real
// ceiling keeps the URL honest about what comes back, and makes a page shorter
// than this a trustworthy end-of-list signal.
const eightfoldPageSize = 10

// eightfoldMaxPages bounds how many pages a single Eightfold tenant may be asked
// for.
//
// The response carries a "count" total, so the normal stop is a short or empty
// page and [pageRepeatGuard] catches a tenant that ignores "start" entirely.
// This is the backstop for the case neither of those can see: a tenant that
// keeps serving *different* full pages forever. At ten postings per page it
// allows 20,000 postings from one company, an order of magnitude above the
// largest tenant registered here (HSBC, ~1,500), so reaching it means something
// is wrong and the adapter stops rather than crawling on.
const eightfoldMaxPages = 2000

// EightfoldCompanies holds the Eightfold tenant slugs this project crawls.
//
// Each entry is the subdomain label of a {slug}.eightfold.ai careers site. Many
// tenants also answer on a branded host — talent.bayer.com, careers.fluor.com,
// explore.jobs.netflix.net — and that branded host is what
// canonicalPositionUrl, and therefore the posting URL, points at. The
// eightfold.ai label is used as the key anyway because it is uniform across
// tenants and is what the API is addressed by; the branded hosts are not
// derivable from it.
//
// **Every slug here answered the list API with postings on two separate runs.**
// That matters more than usual on this platform: roughly three quarters of
// Eightfold tenants answer this endpoint with HTTP 403 and
// `{"message": "Not authorized for PCSX"}` instead, and a walled tenant is
// indistinguishable from a live one by name alone. See
// docs/source-backlog.md for the walled list and what is known about it.
//
// A handful of slugs are the employer's ticker or a product name rather than
// something a job seeker would type, which docs/adding-a-source.md warns about;
// no longer, unambiguous slug exists for them, so they are named here instead:
//
//   - "fcx" is Freeport-McMoRan (talent.fmjobs.com)
//   - "ftr" is Frontier Communications (careers.frontier.com)
//   - "gotinder" is Match Group as a whole (join.matchgroupcareers.com), not
//     Tinder alone
//   - "insight" is Insight Enterprises, the IT reseller
//   - "bcg" is Boston Consulting Group's student/campus board
//     (studenttalent.bcg.com), not its experienced-hire board
var EightfoldCompanies = []string{
	"albemarle",
	"bayer",
	"bcg",
	"coca-colafemsa",
	"costar",
	"faurecia",
	"fcx",
	"fluor",
	"ftr",
	"gotinder",
	"houstonisd",
	"hsbc",
	"insight",
	"libertymutual",
	"netapp",
	"netflix",
	"oxxo",
	"stmicroelectronics",
	"symetra",
	"tevapharm",
	"vale",
}

// eightfoldJobs is the subset of an Eightfold list response this adapter uses.
//
// The response also carries the tenant's whole careers-site configuration —
// branding, facets, geolocation, an empty candidate record — which together
// dwarf the postings; none of it is modelled. What is deliberately absent from
// the model is job_description: the field exists on every position but is empty
// in the list response for every tenant checked, so there is no prose here for
// [internal.CompensationFromText] to read and no pay to publish. Filling it
// would cost one request per posting against the per-job endpoint, which is the
// trade docs/research/ats-platform-survey.md rejects for this platform.
// The tenant's total ("count") is deliberately not modelled even though it is
// free in this same response: see the walk in [Eightfold] for why it is not
// allowed to end the paging.
type eightfoldJobs struct {
	Positions []struct {
		// ID is the Eightfold position id, the number in canonicalPositionUrl.
		ID int64 `json:"id"`

		Name     string `json:"name"`
		Location string `json:"location"`

		// Department and BusinessUnit are both org-unit labels, and which of the
		// two is the coarser one is not consistent across tenants: Bayer files
		// "Medical Affairs & Pharmacovigilance" (department) under
		// "Pharmaceuticals" (business_unit), while HSBC files "Fin Sustain & Grp
		// Ext Comm" (business_unit) under "Finance" (department). They are mapped
		// by name rather than by guessing granularity per tenant, and
		// [internal.Filter.Departments] searches both fields, so the ordering
		// costs a job seeker nothing.
		//
		// Both are [eightfoldText] because Eightfold does not hold their type
		// stable. Fluor — 716 postings, the second-largest tenant registered
		// here — sends department as a JSON array (["Operations & Maintenance"]),
		// while every other tenant sends a bare string and some send null.
		// Modelling it as a Go string would fail the decode for the whole page,
		// and fetchJSON decodes a page at once, so that one field would take down
		// every Fluor posting.
		Department   eightfoldText `json:"department"`
		BusinessUnit eightfoldText `json:"business_unit"`

		// TCreate and TUpdate are Unix seconds.
		TCreate int64 `json:"t_create"`
		TUpdate int64 `json:"t_update"`

		// DisplayJobID is the employer's own requisition number ("877989"), which
		// is what a referral form or a recruiter asks for. It is not the same
		// thing as ID.
		DisplayJobID eightfoldText `json:"display_job_id"`

		// WorkLocationOption is Eightfold's own remote/hybrid/onsite field:
		// "onsite", "hybrid", "remote_local", "remote_global".
		WorkLocationOption string `json:"work_location_option"`

		// CanonicalPositionURL is the posting's public URL, usually on the
		// employer's branded careers host rather than on eightfold.ai.
		CanonicalPositionURL string `json:"canonicalPositionUrl"`
	} `json:"positions"`
}

// eightfoldText decodes a JSON value whose type Eightfold does not hold stable
// into a string.
//
// Kept separate from the other tolerant scalars in this package even though
// several of them look alike today: each one describes what one third-party API
// actually does, and this one has to accept a case none of the others do, an
// array of strings. Anything unreadable becomes the empty string rather than an
// error, because one odd field must never cost a board its postings.
type eightfoldText string

// UnmarshalJSON implements [json.Unmarshaler]. It never reports an error.
func (t *eightfoldText) UnmarshalJSON(data []byte) error {
	*t = ""

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return nil
	}

	switch trimmed[0] {
	case '"':
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return nil
		}

		*t = eightfoldText(text)

	case '[':
		// Fluor's department. Take the first non-empty entry rather than joining:
		// the array holds one label for every posting seen, and a join would
		// invent a compound department name if that ever stops being true.
		var items []string
		if err := json.Unmarshal(data, &items); err != nil {
			return nil
		}

		for _, item := range items {
			if item = strings.TrimSpace(item); item != "" {
				*t = eightfoldText(item)

				break
			}
		}

	case '{':
		// An object is not a label, and rendering its literal JSON would publish
		// "{...}" as an employer's department.

	default:
		*t = eightfoldText(trimmed)
	}

	return nil
}

// String returns the decoded text with surrounding whitespace removed.
func (t eightfoldText) String() string {
	return strings.TrimSpace(string(t))
}

// eightfoldTimestamp converts one of Eightfold's Unix-second stamps to UTC,
// returning the zero time when the field was absent or not a plausible date.
//
// A non-positive value is the common absence, and the upper guard rejects a
// value that is really milliseconds: t_create and t_update are seconds on every
// tenant checked, but a tenant that switched units would otherwise date every
// one of its postings to the year 58000 and quietly satisfy every
// [internal.Filter.PostedSince] query.
func eightfoldTimestamp(seconds int64) time.Time {
	const year2100 = 4102444800 // 2100-01-01T00:00:00Z

	if seconds <= 0 || seconds > year2100 {
		return time.Time{}
	}

	return time.Unix(seconds, 0).UTC()
}

// Eightfold returns all of the job postings for a given company, or an error if
// there was a problem making the request or parsing the response.
//
// A tenant that answers HTTP 403 is not broken infrastructure: Eightfold gates
// this endpoint per tenant, and a walled tenant reports
// `{"message": "Not authorized for PCSX"}` behind that status. The status text
// reaches the operator through fetchJSON's error, which is enough to tell that
// case apart from a 404 (no such tenant) in a `health` run.
func Eightfold(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		var (
			guard pageRepeatGuard
			start int
		)

		for page := 0; page < eightfoldMaxPages; page++ {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())

				return
			}

			doc, err := fetchJSON[eightfoldJobs](ctx, httpClient, "Eightfold", company, jsonRequest{
				URL: "https://" + company + ".eightfold.ai/api/apply/v2/jobs" +
					"?start=" + strconv.Itoa(start) +
					"&num=" + strconv.Itoa(eightfoldPageSize),
			})
			if err != nil {
				yield(nil, err)

				return
			}

			if len(doc.Positions) == 0 {
				return
			}

			ids := make([]string, 0, len(doc.Positions))
			for _, item := range doc.Positions {
				ids = append(ids, strconv.FormatInt(item.ID, 10))
			}

			if guard.repeated(ids) {
				return
			}

			for _, item := range doc.Positions {
				if ctx.Err() != nil {
					yield(nil, ctx.Err())

					return
				}

				url := strings.TrimSpace(item.CanonicalPositionURL)
				if url == "" && item.ID > 0 {
					// Every tenant checked publishes canonicalPositionUrl, but a
					// posting with no URL is useless, and the eightfold.ai host
					// serves the same posting the branded host does.
					url = "https://" + company + ".eightfold.ai/careers/job/" +
						strconv.FormatInt(item.ID, 10)
				}

				location := strings.TrimSpace(item.Location)
				if location == "" {
					location = "unknown/remote"
				}

				posting := &internal.JobPosting{
					Company:       company,
					URL:           url,
					Title:         strings.TrimSpace(item.Name),
					Location:      location,
					Department:    item.Department.String(),
					Team:          item.BusinessUnit.String(),
					WorkplaceType: eightfoldWorkplaceType(item.WorkLocationOption),
					PostedAt:      eightfoldTimestamp(item.TCreate),
					UpdatedAt:     eightfoldTimestamp(item.TUpdate),
					RequisitionID: item.DisplayJobID.String(),
					Source: internal.PostingSource{
						Platform: eightfoldPlatform,
						Key:      company,
					},
				}

				if item.ID > 0 {
					posting.ExternalID = strconv.FormatInt(item.ID, 10)
				}

				if !yield(posting, nil) {
					return
				}
			}

			// A short page is the end of the list. The response also carries a
			// "count" total, and stopping on it would save the one empty request
			// a board whose posting count is an exact multiple of the page size
			// costs. It is deliberately not used for that: a count lower than
			// what the pages actually serve would silently truncate the board,
			// and losing postings is a far worse failure here than spending one
			// more request.
			if len(doc.Positions) < eightfoldPageSize {
				return
			}

			start += len(doc.Positions)
		}
	}
}

// eightfoldWorkplaceType maps Eightfold's work_location_option to the project's
// vocabulary.
//
// The values seen are "onsite", "hybrid" and "remote_local"; "remote_global"
// appears in Eightfold's own configuration. [internal.NormalizeWorkplaceType]
// already reads all four, since it matches "remote" as a substring and checks
// "hybrid" first, so this exists to keep an unrecognised future value from
// becoming anything other than unknown.
func eightfoldWorkplaceType(raw string) internal.WorkplaceType {
	workplace, ok := internal.NormalizeWorkplaceType(raw)
	if !ok {
		return internal.WorkplaceTypeUnknown
	}

	return workplace
}
