package jobpostings

import (
	"context"
)

// GetHopsyJobPostings finds JobPostings found at https://greenhouse.io
func GetHopsyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "hopsy")
}
