package jobpostings

import (
	"context"
)

// GetTransLifelineJobPostings finds JobPostings found at https:/lever.co
func GetTransLifelineJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "translifeline")
}
