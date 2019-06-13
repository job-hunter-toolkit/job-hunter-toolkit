package jobpostings

import (
	"context"
)

// GetMongoDBJobPostings finds JobPostings found at https://greenhouse.io
func GetMongoDBJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "mongodb")
}
