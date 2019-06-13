package jobpostings

import (
	"context"
)

// GetGreenpeaceJobPostings finds JobPostings found at https://greenhouse.io
func GetGreenpeaceJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "greenpeace")
}
