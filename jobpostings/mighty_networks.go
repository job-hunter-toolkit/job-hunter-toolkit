package jobpostings

import (
	"context"
)

// GetMightyNetworksJobPostings finds JobPostings found at https://greenhouse.io
func GetMightyNetworksJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "mightynetworks")
}
