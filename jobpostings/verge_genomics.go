package jobpostings

import (
	"context"
)

// GetVergeGenomicsJobPostings finds JobPostings found at https:/lever.co
func GetVergeGenomicsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "vergegenomics")
}
