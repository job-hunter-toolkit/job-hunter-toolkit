package jobpostings

import (
	"context"
)

// GetVoodooJobPostings finds JobPostings found at https:/lever.co
func GetVoodooJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "voodoo")
}
