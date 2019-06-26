package jobpostings

import (
	"context"
)

// GetBrightBytesJobPostings finds JobPostings found at https://brightbytes.bamboohr.com/jobs/
func GetBrightBytesJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(ctx, "brightbytes")
}
