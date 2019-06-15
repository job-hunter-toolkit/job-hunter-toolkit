package jobpostings

import (
	"context"
)

// GetStarshipJobPostings finds JobPostings found at https:/lever.co
func GetStarshipJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "starship")
}
