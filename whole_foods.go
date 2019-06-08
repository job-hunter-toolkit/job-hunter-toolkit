package jabba

import (
	"context"
)

// GetWholeFoodsJobPostings finds JobPostings found at https://wholefoods.wd5.myworkdayjobs.com/wholefoods
func GetWholeFoodsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://wholefoods.wd5.myworkdayjobs.com/wholefoods")
}