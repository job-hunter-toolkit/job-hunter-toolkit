package jobpostings

import (
	"context"
)

// GetIssuuJobPostings finds JobPostings found at https:/lever.co
func GetIssuuJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "issuu")
}
