package jobpostings

import (
	"context"
)

// GetShopifyJobPostings finds JobPostings found at https:/lever.co
func GetShopifyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "shopify")
}
