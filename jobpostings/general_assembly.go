package jobpostings

import (
	"context"
)

// GetGeneralAssemblyJobPostings finds JobPostings found at https://greenhouse.io
func GetGeneralAssemblyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "generalassembly")
}
