package jobpostings

import (
	"context"
)

// GetQualysJobPostings finds JobPostings found at https://jobvite.com
func GetQualysJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJobviteJobsFor(context.Background(), "qualys")
}
