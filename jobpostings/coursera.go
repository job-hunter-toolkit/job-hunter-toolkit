package jobpostings

import (
	"context"
)

// GetCourseraJobPostings finds JobPostings found at https:/lever.co
func GetCourseraJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "coursera")
}
