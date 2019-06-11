package jobpostings

import (
	"context"
)

// GetMavenClinicJobPostings finds JobPostings found at https://greenhouse.io
func GetMavenClinicJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "mavenclinic")
}
