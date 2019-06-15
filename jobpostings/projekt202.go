package jobpostings

import (
	"context"
)

// GetProjekt202JobPostings finds JobPostings found at https:/lever.co
func GetProjekt202JobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "projekt202")
}
