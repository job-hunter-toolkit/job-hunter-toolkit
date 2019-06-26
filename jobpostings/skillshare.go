package jobpostings

import (
	"context"
)

// GetSkillshareJobPostings finds JobPostings found at https:/lever.co
func GetSkillshareJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "skillshare")
}
