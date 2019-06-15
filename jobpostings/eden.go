package jobpostings

import (
	"context"
)

// GetEdenJobPostings finds JobPostings using https://greenhouse.io
func GetEdenJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "eden18")
}
