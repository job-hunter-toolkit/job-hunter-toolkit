package jobpostings

import (
	"context"
)

// GetPaperspaceJobPostings finds JobPostings found at https:/lever.co
func GetPaperspaceJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "paperspace")
}
