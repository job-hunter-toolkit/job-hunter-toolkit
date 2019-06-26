package jobpostings

import (
	"context"
)

// GetBazaarVoiceJobPostings finds JobPostings found at https:/lever.co
func GetBazaarVoiceJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "bazaarvoice")
}
