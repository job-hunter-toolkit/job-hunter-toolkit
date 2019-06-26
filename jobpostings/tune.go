package jobpostings

import (
	"context"
)

// GetTuneJobPostings finds JobPostings found at https:/lever.co
func GetTuneJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "tune")
}
