package jobpostings

import (
	"context"
)

// GetWikimediaJobPostings finds JobPostings using https://greenhouse.io
func GetWikimediaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "wikimedia")
}
