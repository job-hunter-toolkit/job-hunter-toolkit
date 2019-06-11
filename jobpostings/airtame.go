package jobpostings

import (
	"context"
)

// GetAirtameJobPostings finds JobPostings found at https://greenhouse.io
func GetAirtameJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "airtamejobs")
}
