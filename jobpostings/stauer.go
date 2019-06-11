package jobpostings

import (
	"context"
)

// GetStauerJobPostings finds JobPostings found at https://greenhouse.io
func GetStauerJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "stauer")
}
