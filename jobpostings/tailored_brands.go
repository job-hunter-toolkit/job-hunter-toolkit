package jobpostings

import (
	"context"
)

// GetTailoredBrandsJobPostings finds JobPostings found at https:/jibeapply.com
func GetTailoredBrandsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "tailoredbrands")
}
