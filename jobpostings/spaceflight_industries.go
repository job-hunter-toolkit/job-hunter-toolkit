package jobpostings

import (
	"context"
)

// GetSpaceflightIndustriesJobPostings finds JobPostings found at https://spaceflightindustries.bamboohr.com/jobs/
func GetSpaceflightIndustriesJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "spaceflightindustries")
}
