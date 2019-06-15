package jobpostings

import (
	"context"
)

// GetJournyJobPostings finds JobPostings found at https:/lever.co
func GetJournyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "gojourny")
}
