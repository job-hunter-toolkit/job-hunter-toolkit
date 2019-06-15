package jobpostings

import (
	"context"
)

// GetTripleByteJobPostings finds JobPostings found at https:/lever.co
func GetTripleByteJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "triplebyte")
}
