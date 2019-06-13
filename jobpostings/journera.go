package jobpostings

import (
	"context"
)

// GetJourneraJobPostings finds JobPostings found at https://greenhouse.io
func GetJourneraJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "journera")
}
