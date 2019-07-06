package jobpostings

import (
	"context"
)

// GetChronicledJobPostings finds JobPostings found at https://greenhouse.io
func GetChronicledJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "chronicled")
}
