package jobpostings

import (
	"context"
)

// GetSnapJobPostings finds JobPostings found at https://wd1.myworkdaysite.com/recruiting/snapchat/snap
func GetSnapJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://wd1.myworkdaysite.com/recruiting/snapchat/snap")
}
