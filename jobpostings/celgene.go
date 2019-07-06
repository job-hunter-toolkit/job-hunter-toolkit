package jobpostings

import (
	"context"
)

// GetCelgeneJobPostings finds JobPostings found at https:/jibeapply.com
func GetCelgeneJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "celgene")
}
