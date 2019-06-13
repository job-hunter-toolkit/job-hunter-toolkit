package jobpostings

import (
	"context"
)

// GetTheTradeDeskJobPostings finds JobPostings found at https://greenhouse.io
func GetTheTradeDeskJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "thetradedesk")
}
