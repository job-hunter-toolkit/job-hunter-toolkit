package jabba

import (
	"context"
)

// GetVoxMediaJobPostings finds JobPostings found at https://greenhouse.io
func GetVoxMediaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "voxmedia")
}
