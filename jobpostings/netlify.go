package jobpostings

import (
	"context"
)

// GetNetlifyJobPostings finds JobPostings found at https://greenhouse.io
func GetNetlifyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "netlify")
}
