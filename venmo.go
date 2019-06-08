package jabba

import (
	"context"
)

// GetVenmoJobPostings finds JobPostings found at https://greenhouse.io
func GetVenmoJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "venmo")
}
