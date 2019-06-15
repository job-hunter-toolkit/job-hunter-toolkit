package jobpostings

import (
	"context"
)

// GetZiplineJobPostings finds JobPostings found at https:/lever.co
func GetZiplineJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "flyzipline")
}
