package jobpostings

import (
	"context"
)

// GetOpenCosmosJobPostings finds JobPostings found at https://opencosmos.bamboohr.com/jobs/
func GetOpenCosmosJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "opencosmos")
}
