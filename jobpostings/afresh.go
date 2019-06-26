package jobpostings

import (
	"context"
)

// GetAfreshJobPostings finds JobPostings found at https:/lever.co
func GetAfreshJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "afreshtechnologies")
}
