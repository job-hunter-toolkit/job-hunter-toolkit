package services

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// bambooHRPlatform is the ATS family this file registers, and the value that
// reaches [internal.PostingSource.Platform]. Declared once so the registration
// and the postings cannot drift apart.
const bambooHRPlatform = "bamboohr"

func init() {
	registerBuiltin(bambooHRPlatform, multiJobsFunc(BambooHR, BambooHRCompanies))
}

var BambooHRCompanies = []string{
	"alterian",
	"americanrivers",
	"atomicobject",
	"azerion",
	"beehiiv",
	"britecore",
	"catf",
	"cbf",
	"cdt",
	"charitywater",
	"chimp",
	"coimpact",
	"cortina",
	"crisisgroup",
	"digitalgreen",
	"dockyard",
	"dreamcorps",
	"endeavor",
	"er2",
	"evidenceaction",
	"fauna",
	"freepress",
	"g2",
	"givedirectly",
	"ilrc",
	"interaction",
	"iri",
	"kiva",
	"kodem",
	"lighthouse",
	"malalafund",
	"malarianomore",
	"measurabl",
	"metaltoad",
	"nelp",
	"nextgenamerica",
	"opencosmos",
	"protectdemocracy",
	"r2",
	"refugeesinternational",
	"relayr",
	"securonix",
	"signal1",
	"solidaritycenter",
	"soundstripe",
	"spiralscout",
	"swat",
	"t1cg",
	"themarshallproject",
	"thirdway",
	"trickleup",
	"tttstudios",
	"womenforwomen",
	"zerofox",
	"zyris",
}

type bambooInfo struct {
	Meta struct {
		TotalCount int `json:"totalCount"`
	} `json:"meta"`
	Result []bambooPosting `json:"result"`
}

// bambooPosting is one opening in a tenant's careers list.
//
// Every field here has been decoded since the adapter was written; until now
// only ID, JobOpeningName and Location were ever read, so the department, the
// employment status and both workplace signals were downloaded on every request
// and dropped on the floor.
type bambooPosting struct {
	ID                    string `json:"id"`
	JobOpeningName        string `json:"jobOpeningName"`
	DepartmentID          string `json:"departmentId"`
	DepartmentLabel       string `json:"departmentLabel"`
	EmploymentStatusLabel string `json:"employmentStatusLabel"`
	Location              struct {
		City  string `json:"city"`
		State string `json:"state"`
	} `json:"location"`
	AtsLocation bambooAtsLocation `json:"atsLocation"`

	// IsRemote and LocationType are the two halves of BambooHR's workplace
	// signal, and only one of them is reliably typed: isRemote arrives as a bool
	// on some tenants and as a string on others, which is why it is `any` and
	// read through [anyBool].
	IsRemote     any    `json:"isRemote"`
	LocationType string `json:"locationType"`
}

// bambooAtsLocation is the structured location BambooHR publishes alongside the
// flat city/state pair. Its members are `any` because tenants send strings,
// nulls and numbers interchangeably in them.
type bambooAtsLocation struct {
	Country  any `json:"country"`
	State    any `json:"state"`
	Province any `json:"province"`
	City     any `json:"city"`
}

// anyText renders a JSON value a board types inconsistently as trimmed text.
//
// BambooHR is why it exists. Every member of "atsLocation" is modelled as `any`
// (see bambooInfo) because tenants send those as strings, as null, and as
// numbers; pinning them to a concrete Go type is precisely the mistake that cost
// this project nine large Jibe employers when "meta_data" turned out to be an
// object on some tenants and a bare `false` on others. Anything that is not a
// scalar renders as "", never as Go's %v spelling of a map, which would put a
// literal "map[]" into a posting's location.
func anyText(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case bool:
		return strconv.FormatBool(typed)
	case float64:
		// encoding/json decodes every JSON number as float64, so an integer id
		// has to be printed without the trailing ".0" that %v would give it.
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case []any:
		// A board that publishes a bare value on most tenants and a list of them
		// on others: the first element is what the single-valued form means.
		if len(typed) > 0 {
			return anyText(typed[0])
		}
	}

	return ""
}

// anyBool reads a flag a board types inconsistently, reporting false as its
// second result when the board published nothing usable.
//
// The two results are kept apart because [internal.JobPosting.Remote] is a
// *bool for exactly that reason: a nil Remote falls back to the location-text
// heuristic, while a false one is the board stating a fact. Collapsing "absent"
// into "false" would silently switch that heuristic off for every posting on
// every board that omits the field.
func anyBool(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case float64:
		return typed != 0, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "t", "yes", "y", "1":
			return true, true
		case "false", "f", "no", "n", "0":
			return false, true
		}
	}

	return false, false
}

// bambooLocation renders the posting's location, falling back to the richer
// "atsLocation" object when the flat city/state pair is empty.
//
// The flat pair is left exactly as it was, byte for byte, whenever it holds
// anything at all. The fallback only replaces the old behaviour of labelling
// every posting with no city and no state "remote" — a guess, and a wrong one
// for a Berlin office that merely has no state to report, when atsLocation was
// sitting in the same response saying "Berlin, Germany". The placeholder still
// covers a tenant that published no location anywhere.
func bambooLocation(job bambooPosting) string {
	if flat := fmt.Sprintf("%s, %s", job.Location.City, job.Location.State); flat != ", " {
		return flat
	}

	parts := make([]string, 0, 3)

	for _, part := range []string{
		anyText(job.AtsLocation.City),
		// State and province are the same slot spelled two ways; a tenant fills
		// in whichever its country uses, so the first non-empty one wins.
		cmp.Or(anyText(job.AtsLocation.State), anyText(job.AtsLocation.Province)),
		anyText(job.AtsLocation.Country),
	} {
		if part != "" {
			parts = append(parts, part)
		}
	}

	if len(parts) == 0 {
		return "remote"
	}

	return strings.Join(parts, ", ")
}

// bambooWorkplaceType maps BambooHR's two workplace signals onto the canonical
// vocabulary.
//
// "locationType" is the structured field and is preferred. isRemote is only
// consulted when locationType says nothing, and only in the affirmative:
// isRemote=false means the role is not remote, which is not the same claim as
// "the employee must be in an office", and inventing the stronger claim would
// put every hybrid posting on a tenant that omits locationType into
// --workplace-type=onsite.
func bambooWorkplaceType(job bambooPosting) internal.WorkplaceType {
	if workplace, ok := internal.NormalizeWorkplaceType(job.LocationType); ok {
		return workplace
	}

	if remote, published := anyBool(job.IsRemote); published && remote {
		return internal.WorkplaceTypeRemote
	}

	return internal.WorkplaceTypeUnknown
}

func BambooHR(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		url := fmt.Sprintf("https://%s.bamboohr.com/careers/list", company)

		// Note: an unknown BambooHR tenant answers with a redirect to bamboohr.com's
		// marketing page rather than a 404, so a dead slug surfaces here as a
		// decode failure on HTML. That is the expected signature, not a bug.
		doc, err := fetchJSON[bambooInfo](ctx, httpClient, "BambooHR", company, jsonRequest{URL: url})
		if err != nil {
			yield(nil, err)

			return
		}

		// If there are no job postings, exit the loop
		if doc.Meta.TotalCount == 0 {
			return
		}

		// Iterate over the job postings and yield each one
		for _, job := range doc.Result {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			posting := &internal.JobPosting{
				Company:  company,
				URL:      fmt.Sprintf("%s?id=%s", url, job.ID), // Construct the full URL for the job posting
				Title:    job.JobOpeningName,
				Location: bambooLocation(job),

				Department:    strings.TrimSpace(job.DepartmentLabel),
				WorkplaceType: bambooWorkplaceType(job),
				ExternalID:    job.ID,
				Source:        internal.PostingSource{Platform: bambooHRPlatform, Key: company},
			}

			// employmentStatusLabel is a per-tenant label, not an enum: "Full-Time",
			// "Regular Full-Time", "PT", "Contractor". An unrecognised one is left
			// empty rather than guessed, because a filter cannot tell a wrong
			// answer from a right one while an absent field is visibly absent.
			if employment, ok := internal.NormalizeEmploymentType(job.EmploymentStatusLabel); ok {
				posting.EmploymentType = employment
			}

			// Only recorded when the tenant actually published the flag. A nil
			// Remote is what lets [internal.JobPosting.IsRemote] fall back to
			// reading the location text.
			if remote, published := anyBool(job.IsRemote); published {
				posting.Remote = &remote
			}

			if !yield(posting, nil) {
				return
			}
		}

		// TODO: handle pagination if the API supports it
	}
}
