package jobpostings

import (
	"context"
)

// GetQualtricsJobPostings finds JobPostings found at https://greenhouse.io
func GetQualtricsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "qualtrics")
}
