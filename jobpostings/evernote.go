package jobpostings

import (
	"context"
)

// GetEvernoteJobPostings finds JobPostings found at https://greenhouse.io
func GetEvernoteJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "evernote")
}
