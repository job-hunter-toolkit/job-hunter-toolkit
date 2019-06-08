package jobpostings

import (
	"context"
)

// GetCiscoMerakiJobPostings finds JobPostings found at https://greenhouse.io
func GetCiscoMerakiJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "ciscomeraki")
}
