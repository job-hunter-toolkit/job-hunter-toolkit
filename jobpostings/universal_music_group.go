package jobpostings

import (
	"context"
)

// GetUniversalMusicGroupJobPostings finds JobPostings found at https://jobvite.com
func GetUniversalMusicGroupJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJobviteJobsFor(ctx, "universalmusicgroup")
}
