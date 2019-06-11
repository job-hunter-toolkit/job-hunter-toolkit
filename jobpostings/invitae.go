package jobpostings

import (
	"context"
)

// GetInvitaeJobPostings finds JobPostings found at https://greenhouse.io
func GetInvitaeJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "invitae")
}
