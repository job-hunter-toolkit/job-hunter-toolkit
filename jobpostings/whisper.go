package jobpostings

import (
	"context"
)

// GetWhisperJobPostings finds JobPostings found at https://greenhouse.io
func GetWhisperJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "whisperai")
}
