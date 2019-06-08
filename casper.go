package jabba

import (
	"context"
)

// GetCasperJobPostings finds JobPostings found at https://greenhouse.io
func GetCasperJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "casper")
}
