package jabba

import (
	"context"
)

// GetMastercardJobPostings finds JobPostings using https://mastercard.wd1.myworkdayjobs.com/CorporateCareers
func GetMastercardJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://mastercard.wd1.myworkdayjobs.com/CorporateCareers")
}