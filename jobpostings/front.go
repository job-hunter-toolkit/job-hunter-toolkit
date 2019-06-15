package jobpostings

import (
	"context"
)

// GetFrontJobPostings finds JobPostings found at https:/lever.co
func GetFrontJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "frontapp")
}
