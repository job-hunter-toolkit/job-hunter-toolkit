package jobpostings

import (
	"context"
)

// GetFuboTVJobPostings finds JobPostings found at https://greenhouse.io
func GetFuboTVJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "fubotv")
}
