package jobpostings

import (
	"context"
)

// GetHelloAlfredJobPostings finds JobPostings found at https://greenhouse.io
func GetHelloAlfredJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "helloalfred58")
}
