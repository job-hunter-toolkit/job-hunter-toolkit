package jobpostings

import (
	"context"
)

// GetAzerionJobPostings finds JobPostings found at https://azerion.bamboohr.com/jobs/
func GetAzerionJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(ctx, "azerion")
}
