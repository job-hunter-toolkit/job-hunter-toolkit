package jobpostings

import (
	"context"
)

// GetKhanAcademyJobPostings finds JobPostings found at https://greenhouse.io
func GetKhanAcademyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "khanacademy")
}
