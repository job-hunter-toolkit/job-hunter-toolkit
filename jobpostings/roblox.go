package jobpostings

import (
	"context"
)

// GetRobloxJobPostings finds JobPostings using https://greenhouse.io
func GetRobloxJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "roblox")
}
