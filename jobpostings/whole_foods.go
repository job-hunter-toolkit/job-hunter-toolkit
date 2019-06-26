package jobpostings

import (
	"context"
)

// GetWholeFoodsJobPostings finds JobPostings found at https://wholefoods.wd5.myworkdayjobs.com/wholefoods
func GetWholeFoodsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://wholefoods.wd5.myworkdayjobs.com/wholefoods")
}
