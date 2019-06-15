package jobpostings

import (
	"context"
)

// GetMattermostJobPostings finds JobPostings found at https:/lever.co
func GetMattermostJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "mattermost")
}
