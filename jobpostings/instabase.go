package jobpostings

import (
	"context"
)

// GetInstabaseJobPostings finds JobPostings found at https://greenhouse.io
func GetInstabaseJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "instabase")
}
