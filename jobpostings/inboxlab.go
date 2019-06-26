package jobpostings

import (
	"context"
)

// GetInboxLabJobPostings finds JobPostings found at https:/lever.co
func GetInboxLabJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "inboxlab")
}
