package jobpostings

import (
	"context"
)

// GetCruxJobPostings finds JobPostings found at https:/lever.co
func GetCruxJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "cruxinformatics")
}
