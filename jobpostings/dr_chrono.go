package jobpostings

import (
	"context"
)

// GetDrChronoJobPostings finds JobPostings found at https:/lever.co
func GetDrChronoJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "drchrono")
}
