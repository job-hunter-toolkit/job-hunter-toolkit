package services

import (
	"context"
	"fmt"
	"net/http"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

func init() {
	registerBuiltin(multiJobsFunc(BambooHR, BambooHRCompanies))
}

var BambooHRCompanies = []string{
	"alterian",
	"americanrivers",
	"atomicobject",
	"azerion",
	"britecore",
	"catf",
	"cbf",
	"cdt",
	"charitywater",
	"chimp",
	"coimpact",
	"crisisgroup",
	"digitalgreen",
	"dockyard",
	"dreamcorps",
	"endeavor",
	"evidenceaction",
	"fauna",
	"freepress",
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
	"refugeesinternational",
	"relayr",
	"securonix",
	"solidaritycenter",
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
	Result []struct {
		ID                    string `json:"id"`
		JobOpeningName        string `json:"jobOpeningName"`
		DepartmentID          string `json:"departmentId"`
		DepartmentLabel       string `json:"departmentLabel"`
		EmploymentStatusLabel string `json:"employmentStatusLabel"`
		Location              struct {
			City  string `json:"city"`
			State string `json:"state"`
		} `json:"location"`
		AtsLocation struct {
			Country  any `json:"country"`
			State    any `json:"state"`
			Province any `json:"province"`
			City     any `json:"city"`
		} `json:"atsLocation"`
		IsRemote     any    `json:"isRemote"`
		LocationType string `json:"locationType"`
	} `json:"result"`
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

			locationStr := fmt.Sprintf("%s, %s", job.Location.City, job.Location.State)
			if locationStr == ", " {
				locationStr = "remote"
			}

			if !yield(&internal.JobPosting{
				Company:  company,
				URL:      fmt.Sprintf("%s?id=%s", url, job.ID), // Construct the full URL for the job posting
				Title:    job.JobOpeningName,
				Location: locationStr,
			}, nil) {
				return
			}
		}

		// TODO: handle pagination if the API supports it
	}
}
