package jobpostings

import (
	"context"
)

// GetNexTravelobPostings finds JobPostings found at https:/lever.co
func GetNexTravelobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "nextravel")
}
