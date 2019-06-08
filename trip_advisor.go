package jabba

import (
	"context"
)

// GetTripAdvisorJobPostings finds JobPostings found at https://greenhouse.io
func GetTripAdvisorJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "tripadvisor")
}
