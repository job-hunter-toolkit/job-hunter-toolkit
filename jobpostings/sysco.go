package jobpostings

import (
	"context"
)

// GetSyscoJobPostings finds JobPostings found at https:/jibeapply.com
func GetSyscoJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "sysco")
}
