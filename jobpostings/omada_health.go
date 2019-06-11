package jobpostings

import (
	"context"
)

// GetOmadaHealthJobPostings finds JobPostings found at https://greenhouse.io
func GetOmadaHealthJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "omadahealth")
}
