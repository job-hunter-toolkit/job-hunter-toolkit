package jobpostings

import (
	"context"
)

// GetAdobeJobPostings finds JobPostings using https://adobe.wd5.myworkdayjobs.com/external_experienced
func GetAdobeJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://adobe.wd5.myworkdayjobs.com/external_experienced")
}
