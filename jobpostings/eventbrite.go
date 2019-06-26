package jobpostings

import (
	"context"
)

// GetEventbriteJobPostings finds JobPostings found at https:/lever.co
func GetEventbriteJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "eventbrite")
}
