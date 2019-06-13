package jobpostings

import (
	"context"
)

// GetSproutSocialobPostings finds JobPostings found at https://greenhouse.io
func GetSproutSocialobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "sproutsocial")
}
