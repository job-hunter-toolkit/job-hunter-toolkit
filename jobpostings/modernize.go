package jobpostings

import (
	"context"
)

// GetModernizeJobPostings finds JobPostings found at https://greenhouse.io
func GetModernizeJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "modernize")
}
