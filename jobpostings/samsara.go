package jobpostings

import (
	"context"
)

// GetSamsaraJobPostings finds JobPostings found at https://greenhouse.io
func GetSamsaraJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "samsara")
}
