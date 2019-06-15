package jobpostings

import (
	"context"
)

// GetDeliverooJobPostings finds JobPostings using https://greenhouse.io
func GetDeliverooJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "deliveroo")
}
