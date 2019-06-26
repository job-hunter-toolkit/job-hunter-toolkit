package jobpostings

import (
	"context"
)

// GetImageworksJobPostings finds JobPostings using https://spe.wd1.myworkdayjobs.com/Imageworks
func GetImageworksJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://spe.wd1.myworkdayjobs.com/Imageworks")
}
