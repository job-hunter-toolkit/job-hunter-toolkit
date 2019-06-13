package jobpostings

import (
	"context"
)

// GetZyrisJobPostings finds JobPostings found at https://zyris.bamboohr.com/jobs/
func GetZyrisJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "zyris")
}
