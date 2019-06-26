package jobpostings

import (
	"context"
)

// GetStreamlabsJobPostings finds JobPostings found at https:/lever.co
func GetStreamlabsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "streamlabs")
}
