package jobpostings

import (
	"context"
)

// GetPachydermJobPostings finds JobPostings found at https:/lever.co
func GetPachydermJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "pachyderm")
}
