package services

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// smartRecruitersPlatform is the ATS family this file registers, and the value
// that reaches [internal.PostingSource.Platform].
const smartRecruitersPlatform = "smartrecruiters"

func init() {
	registerBuiltin(smartRecruitersPlatform, multiJobsFunc(SmartRecruiters, SmartRecruitersCompanies))
}

var SmartRecruitersCompanies = []string{
	"Accor",
	"Alorica",
	"Armis",
	"ASOS",
	"Auto1",
	"AveryDennison",
	"BoschGroup",
	"ChristianBrothersAutomotive",
	"Chubb2",
	"CityFibre",
	"Colliers",
	"Continental",
	"CrunchFitness",
	"DeliveryHero",
	"DeloitteNetherlands",
	"Dominos",
	"Equinox",
	"Expeditors",
	"FosterFarms",
	"FrasersGroup",
	"Gameloft",
	"GEHealthcare2",
	"GEICO",
	"HaltonHealthcare1",
	"ifs1",
	"Justworks",
	"JYSK",
	"KimberlyClark",
	"KittitasValleyHealthcare",
	"LVMH",
	"McDonaldsCorporation",
	"Nine",
	"northwesternmedicine",
	"NorwegianCruiseLine",
	"optiv",
	"ORICPharmaceuticals",
	"PaloAltoNetworks2",
	"PharmaCannis",
	"Primark",
	"PublicStorage",
	"RaisingCanes",
	"SanaCommerce",
	"ServiceNow",
	"Sixt",
	"Sodexo",
	"SonicAutomotive",
	"TheNielsenCompany",
	"Tipico",
	"TTEC",
	"TurnerConstruction",
	"visa",
	"Wise",
	"wtw",
	"Xplor",
}

type smartRecruitersJobs struct {
	Offset     int                      `json:"offset"`
	Limit      int                      `json:"limit"`
	TotalFound int                      `json:"totalFound"`
	Content    []smartRecruitersPosting `json:"content"`
}

// smartRecruitersPosting is one opening in a tenant's public posting list.
//
// Everything below ID, Name and Location is enrichment that this project has
// never decoded from a live SmartRecruiters response: the field names come from
// the vendor's published Posting API rather than from a body captured here, and
// this container cannot reach api.smartrecruiters.com to check. Two consequences
// are designed for rather than hoped away.
//
// A name that turns out to be wrong yields an empty field, which is safe, and
// which is why nothing downstream treats an absent value as a "no".
//
// A *type* that turns out to be wrong is not safe: encoding/json fails the whole
// decode, and one failed decode takes out an entire tenant. That is not a
// hypothetical here — modelling Jibe's "meta_data" as a fixed struct silently
// disabled nine large employers when it turned out to be a bare `false` on some
// tenants (see jibeJobs, and the regression test that pins it). So every
// unverified field is typed to be unfailable: object-shaped ones through
// [smartRecruitersLabel], which accepts an object or a bare string, and scalars
// as `any`, read through [anyText] and [anyBool]. The cost is a little ceremony;
// the alternative cost is 54 tenants disappearing from a crawl with no error
// anyone would connect to this change.
type smartRecruitersPosting struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Location struct {
		City    string `json:"city"`
		Region  string `json:"region"`
		Country string `json:"country"`

		// Remote is the board's structured remote flag.
		Remote any `json:"remote"`
	} `json:"location"`

	// RefNumber is the employer's own requisition number and ReleasedDate is
	// when the posting went live, in ISO 8601.
	RefNumber    any `json:"refNumber"`
	ReleasedDate any `json:"releasedDate"`

	Department       smartRecruitersLabel `json:"department"`
	Function         smartRecruitersLabel `json:"function"`
	ExperienceLevel  smartRecruitersLabel `json:"experienceLevel"`
	TypeOfEmployment smartRecruitersLabel `json:"typeOfEmployment"`
}

// smartRecruitersLabel is one of SmartRecruiters' id/label pairs.
type smartRecruitersLabel struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// UnmarshalJSON accepts the documented object, a bare string, or anything else
// at all, and never reports an error.
//
// See [smartRecruitersPosting] for why: an enrichment field must not be able to
// fail a tenant's decode. A shape this cannot read leaves the label empty, which
// is indistinguishable from a tenant that publishes no department — a state the
// rest of the pipeline already handles — whereas returning an error would delete
// every posting the tenant has.
func (l *smartRecruitersLabel) UnmarshalJSON(data []byte) error {
	var label string
	if err := json.Unmarshal(data, &label); err == nil {
		l.Label = strings.TrimSpace(label)

		return nil
	}

	var object struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	}

	if err := json.Unmarshal(data, &object); err != nil {
		return nil
	}

	l.ID, l.Label = object.ID, strings.TrimSpace(object.Label)

	return nil
}

// smartRecruitersMaxPages bounds how many pages a single SmartRecruiters tenant
// may be asked for.
//
// This loop's only stop conditions were an empty page and the tenant's own
// "totalFound". Both are supplied by the server, so a tenant reporting a
// totalFound of ten million while serving ten postings a page would issue a
// million requests against api.smartrecruiters.com, a shared host, and yield
// nothing but duplicates. The other paginating adapters in this package were
// given explicit ceilings after exactly that failure was reproduced against
// them; this one was missed because no ceiling looks necessary while the server
// is behaving. At 100 postings a page this allows 50,000 per tenant, well beyond
// the largest SmartRecruiters employer observed here.
const smartRecruitersMaxPages = 500

// smartRecruitersPageSize is how many postings each request asks for.
//
// The API's documented maximum, and ten times its default. This request used to
// send no "limit" at all, so all 54 tenants were paged at the default of ten,
// and a 900-posting employer cost 90 requests to api.smartrecruiters.com — one
// host shared by every tenant — where 9 would do. Asking for more per request is
// not extra pressure on that host, it is an order of magnitude less of it.
const smartRecruitersPageSize = 100

// smartRecruitersPage fetches one page of SmartRecruiters postings.
func smartRecruitersPage(ctx context.Context, httpClient *http.Client, company string, offset int) (*smartRecruitersJobs, error) {
	query := url.Values{
		"offset": {strconv.Itoa(offset)},
		"limit":  {strconv.Itoa(smartRecruitersPageSize)},
	}

	return fetchJSON[smartRecruitersJobs](ctx, httpClient, "SmartRecruiters", company, jsonRequest{
		URL: "https://api.smartrecruiters.com/v1/companies/" + company + "/postings?" + query.Encode(),
	})
}

// smartRecruitersLayouts are the timestamp spellings "releasedDate" has been
// documented with. RFC 3339 covers the common form, including the fractional
// seconds and the "Z" that SmartRecruiters sends; the bare date is a fallback
// for a tenant that publishes a day with no time on it.
var smartRecruitersLayouts = []string{
	time.RFC3339,
	"2006-01-02",
}

// smartRecruitersReleasedAt converts the posting's release timestamp to UTC,
// reporting false when it is missing or in a spelling this does not know.
//
// Adapters store UTC so that comparing two postings from two platforms is a
// comparison of instants rather than of formats; an unparseable value is left
// absent rather than defaulted to the crawl time, which would date every posting
// on the platform to today and quietly fill a `--posted-since 7d` query.
func smartRecruitersReleasedAt(raw any) (time.Time, bool) {
	text := anyText(raw)
	if text == "" {
		return time.Time{}, false
	}

	for _, layout := range smartRecruitersLayouts {
		if released, err := time.Parse(layout, text); err == nil {
			return released.UTC(), true
		}
	}

	return time.Time{}, false
}

// SmartRecruiters returns the job postings for a company hosted on
// SmartRecruiters.
//
// Note that this API answers HTTP 200 for any company name, real or not, with
// totalFound of zero. A zero-posting result therefore does not distinguish "not
// hiring" from "no such tenant", which matters when verifying a new entry.
func SmartRecruiters(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		var pages pageRepeatGuard

		offset := 0

		for range smartRecruitersMaxPages {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())

				return
			}

			doc, err := smartRecruitersPage(ctx, httpClient, company, offset)
			if err != nil {
				yield(nil, err)

				return
			}

			// An empty page ends pagination. Checked before advancing the offset
			// because a zero-length page would otherwise leave the offset
			// unchanged and loop forever.
			if len(doc.Content) == 0 {
				return
			}

			// A tenant that ignores "offset" answers every request with the same
			// first page. Without this the loop would run to smartRecruitersMaxPages
			// emitting duplicates, which Dedupe would then hide, so the only visible
			// symptom would be a slow crawl.
			ids := make([]string, 0, len(doc.Content))
			for _, item := range doc.Content {
				ids = append(ids, item.ID)
			}

			if pages.repeated(ids) {
				return
			}

			for _, item := range doc.Content {
				if ctx.Err() != nil {
					yield(nil, ctx.Err())

					return
				}

				location := strings.Join([]string{
					item.Location.City,
					item.Location.Region,
					item.Location.Country,
				}, ",")

				posting := &internal.JobPosting{
					Company:  company,
					URL:      fmt.Sprintf("https://jobs.smartrecruiters.com/%s/%s", company, item.ID),
					Title:    strings.TrimSpace(item.Name),
					Location: location,

					// "function" is the standardised job function rather than the
					// employer's own org unit, so it is a fallback and not a Team:
					// calling it one would claim a hierarchy ("Platform inside
					// Engineering") that SmartRecruiters never published. As a
					// fallback it still puts the word a job seeker would type
					// where [internal.Filter.Departments] looks for it.
					Department:    cmp.Or(item.Department.Label, item.Function.Label),
					Seniority:     cmp.Or(item.ExperienceLevel.Label, item.ExperienceLevel.ID),
					RequisitionID: anyText(item.RefNumber),
					ExternalID:    item.ID,
					Source:        internal.PostingSource{Platform: smartRecruitersPlatform, Key: company},
				}

				if employment, ok := internal.NormalizeEmploymentType(item.TypeOfEmployment.Label); ok {
					posting.EmploymentType = employment
				}

				if released, ok := smartRecruitersReleasedAt(item.ReleasedDate); ok {
					posting.PostedAt = released
				}

				// Only in the affirmative: SmartRecruiters publishes no
				// hybrid/onsite distinction in this field, so remote=false says
				// "not fully remote" and not "must be in an office".
				if remote, published := anyBool(item.Location.Remote); published && remote {
					posting.Remote = &remote
					posting.WorkplaceType = internal.WorkplaceTypeRemote
				}

				if !yield(posting, nil) {
					return
				}
			}

			offset += len(doc.Content)

			if offset >= doc.TotalFound {
				return
			}
		}

		yield(nil, fmt.Errorf("SmartRecruiters postings for %q exceeded %d pages; refusing to keep paginating",
			company, smartRecruitersMaxPages))
	}
}
