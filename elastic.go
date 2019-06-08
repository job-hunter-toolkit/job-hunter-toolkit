package jabba

import (
	"context"
)

// GetElasticJobPostings finds JobPostings found at https://greenhouse.io
func GetElasticJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "elastic")
}
