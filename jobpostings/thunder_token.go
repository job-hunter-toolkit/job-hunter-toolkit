package jobpostings

import (
	"context"
)

// GetThunderTokenJobPostings finds JobPostings found at https:/lever.co
func GetThunderTokenJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "thundertoken")
}
