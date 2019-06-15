package jobpostings

import (
	"context"
)

// GetDivvyHomesJobPostings finds JobPostings found at https:/lever.co
func GetDivvyHomesJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "divvyhomes")
}
