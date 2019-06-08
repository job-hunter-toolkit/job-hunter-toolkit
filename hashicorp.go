package jabba

import (
	"context"
)

// GetHashiCorpJobPostings finds JobPostings found at https://greenhouse.io
func GetHashiCorpJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "hashicorp")
}
