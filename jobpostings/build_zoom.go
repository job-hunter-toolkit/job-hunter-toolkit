package jobpostings

import (
	"context"
)

// GetBuildZoomJobPostings finds JobPostings found at https:/lever.co
func GetBuildZoomJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "buildzoom")
}
