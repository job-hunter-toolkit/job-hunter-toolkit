package jobpostings

import (
	"context"
)

// GetScalewayJobPostings finds JobPostings found at https://jobs.lever.co/scaleway
func GetScalewayJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "scaleway")
}
