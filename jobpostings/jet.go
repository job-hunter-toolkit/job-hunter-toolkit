package jobpostings

import (
	"context"
)

// GetJetJobPostings finds JobPostings found at https://greenhouse.io
func GetJetJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "jet")
}
