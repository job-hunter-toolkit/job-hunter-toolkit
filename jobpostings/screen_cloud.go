package jobpostings

import (
	"context"
)

// GetScreenCloudJobPostings finds JobPostings found at https://screencloud.bamboohr.com/jobs/
func GetScreenCloudJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(ctx, "screencloud")
}
