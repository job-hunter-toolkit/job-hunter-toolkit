package jobpostings

import (
	"context"
)

// GetSnapdocsJobPostings finds JobPostings found at https:/lever.co
func GetSnapdocsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "snapdocs")
}
