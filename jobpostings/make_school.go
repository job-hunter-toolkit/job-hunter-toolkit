package jobpostings

import (
	"context"
)

// GetMakeSchoolJobPostings finds JobPostings found at https:/lever.co
func GetMakeSchoolJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "makeschool")
}
