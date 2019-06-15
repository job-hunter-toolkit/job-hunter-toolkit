package jobpostings

import (
	"context"
)

// GetVendJobPostings finds JobPostings using https://greenhouse.io
func GetVendJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "vend")
}
