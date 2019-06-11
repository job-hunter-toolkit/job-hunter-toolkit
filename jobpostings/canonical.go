package jobpostings

import (
	"context"
)

// GetCanonicalJobPostings finds JobPostings found at https://greenhouse.io
func GetCanonicalJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "canonical")
}
