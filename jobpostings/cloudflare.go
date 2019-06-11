package jobpostings

import (
	"context"
)

// GetCloudflareJobPostings finds JobPostings found at https://greenhouse.io
func GetCloudflareJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "cloudflare")
}
