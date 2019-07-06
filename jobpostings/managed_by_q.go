package jobpostings

import (
	"context"
)

// GetManageBbyQJobPostings finds JobPostings found at https://greenhouse.io
func GetManageBbyQJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "managedbyq")
}
