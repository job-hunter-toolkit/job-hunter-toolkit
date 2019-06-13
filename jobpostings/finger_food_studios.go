package jobpostings

import (
	"context"
)

// GetFingerFoodStudiosJobPostings finds JobPostings found at https://fingerfoodstudios.bamboohr.com/jobs/
func GetFingerFoodStudiosJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "fingerfoodstudios")
}
