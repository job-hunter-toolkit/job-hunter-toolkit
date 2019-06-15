package jobpostings

import (
	"context"
)

// GetZendeskJobPostings finds JobPostings found at https:/lever.co
func GetZendeskJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "zendesk")
}
