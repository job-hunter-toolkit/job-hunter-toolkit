package jobpostings

import (
	"context"
)

// GetSmartsheetJobPostings finds JobPostings found at https://greenhouse.io
func GetSmartsheetJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "smartsheet")
}
