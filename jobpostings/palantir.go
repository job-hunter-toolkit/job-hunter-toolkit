package jobpostings

import (
	"context"
)

// GetPalantirJobPostings finds JobPostings found at https://palantir.bamboohr.com/jobs/
func GetPalantirJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "palantir")
}
