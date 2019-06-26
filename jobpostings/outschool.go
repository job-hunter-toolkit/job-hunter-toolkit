package jobpostings

import (
	"context"
)

// GetOutschoolJobPostings finds JobPostings found at https:/lever.co
func GetOutschoolJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "outschool")
}
