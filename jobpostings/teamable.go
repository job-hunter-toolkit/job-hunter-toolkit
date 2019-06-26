package jobpostings

import (
	"context"
)

// GetTeamableJobPostings finds JobPostings found at https:/lever.co
func GetTeamableJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "teamable")
}
