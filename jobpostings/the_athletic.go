package jobpostings

import (
	"context"
)

// GetTheAthleticJobPostings finds JobPostings found at https:/lever.co
func GetTheAthleticJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "theathletic")
}
