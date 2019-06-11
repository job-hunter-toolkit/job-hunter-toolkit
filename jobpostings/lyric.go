package jobpostings

import (
	"context"
)

// GetLyricJobPostings finds JobPostings found at https://greenhouse.io
func GetLyricJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "lyric")
}
