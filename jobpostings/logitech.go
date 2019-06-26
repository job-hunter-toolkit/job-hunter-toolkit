package jobpostings

import (
	"context"
)

// GetLogitechJobPostings finds JobPostings found at https://jobvite.com
func GetLogitechJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJobviteJobsFor(ctx, "logitech")
}
