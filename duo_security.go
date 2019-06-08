package jabba

import (
	"context"
)

// GetDuoSecurityJobPostings finds JobPostings using https://greenhouse.io
func GetDuoSecurityJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "duosecurity")
}
