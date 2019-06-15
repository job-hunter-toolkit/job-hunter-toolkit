package jobpostings

import (
	"context"
)

// GetSkydioJobPostings finds JobPostings found at https:/lever.co
func GetSkydioJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "skydio")
}
