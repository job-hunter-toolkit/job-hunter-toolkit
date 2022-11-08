package jobpostings

import (
	"context"
)

// GetSharkNinjaJobPostings finds JobPostings found at https://www.sharkninja.com/careers/
func GetSharkNinjaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "sharkninja")
}
