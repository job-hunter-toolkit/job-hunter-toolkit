package jobpostings

import (
	"context"
)

// GetKinnekJobPostings finds JobPostings found at https://greenhouse.io
func GetKinnekJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "kinnek")
}
