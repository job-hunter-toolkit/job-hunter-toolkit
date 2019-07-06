package jobpostings

import (
	"context"
)

// GetVisaJobPostings finds JobPostings found at https://smartrecruiters.com
func GetVisaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getSmartRecruitersJobsPostingsFor(ctx, "visa")
}
