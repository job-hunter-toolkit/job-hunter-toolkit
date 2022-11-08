package jobpostings

import (
	"context"
)

// GetNetflixJobPostings finds JobPostings found at https://jobs.lever.co/netflix
func GetNetflixJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "netflix")
}
