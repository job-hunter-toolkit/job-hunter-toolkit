package jobpostings

import (
	"context"
)

// GetSwissBorgJobPostings finds JobPostings found at https:/lever.co
func GetSwissBorgJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "swissborg")
}
