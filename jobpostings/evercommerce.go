package jobpostings

import (
	"context"
)

// GetEvercommerceJobPostings finds JobPostings found at https://evercommerce.bamboohr.com/jobs/
func GetEvercommerceJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "evercommerce")
}
