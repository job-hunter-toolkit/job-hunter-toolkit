package jobpostings

import (
	"context"
)

// GetAquabyteJobPostings finds JobPostings found at https:/lever.co
func GetAquabyteJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "aquabyte")
}
