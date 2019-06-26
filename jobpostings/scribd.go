package jobpostings

import (
	"context"
)

// GetScribdJobPostings finds JobPostings found at https:/lever.co
func GetScribdJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "scribd")
}
