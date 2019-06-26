package jobpostings

import (
	"context"
)

// GetRoosterTeethJobPostings finds JobPostings found at https:/lever.co
func GetRoosterTeethJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "roosterteeth")
}
