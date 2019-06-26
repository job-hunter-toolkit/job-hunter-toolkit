package jobpostings

import (
	"context"
)

// GetOptivJobPostings finds JobPostings found at https://smartrecruiters.com
func GetOptivJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getSmartRecruitersJobsPostingsFor(ctx, "optiv")
}
