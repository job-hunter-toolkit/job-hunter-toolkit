package jobpostings

import (
	"context"
)

// GetShopKeepJobPostings finds JobPostings using https://greenhouse.io
func GetShopKeepJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "shopkeep")
}
