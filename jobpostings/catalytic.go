package jobpostings

import (
	"context"
)

// GetCatalyticJobPostings finds JobPostings found at https://catalyticds.bamboohr.com/jobs/
func GetCatalyticJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(ctx, "catalyticds")
}
