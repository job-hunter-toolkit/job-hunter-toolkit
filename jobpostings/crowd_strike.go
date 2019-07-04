package jobpostings

import (
	"context"
)

// GetCrowdStrikeJobPostings finds JobPostings found at https://jobvite.com
func GetCrowdStrikeJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJobviteJobsFor(ctx, "crowdstrike")
}