package jobpostings

import (
	"context"
)

// GetPersistIQJobPostings finds JobPostings found at https:/lever.co
func GetPersistIQJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "persistiq")
}
