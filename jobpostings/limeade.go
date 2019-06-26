package jobpostings

import (
	"context"
)

// GetLimeadeJobPostings finds JobPostings found at https://jobvite.com
func GetLimeadeJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJobviteJobsFor(ctx, "limeade")
}
