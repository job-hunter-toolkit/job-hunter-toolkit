package jobpostings

import (
	"context"
)

// GetReplateJobPostings finds JobPostings found at https:/lever.co
func GetReplateJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "replate")
}
