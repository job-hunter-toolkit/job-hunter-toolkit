package jobpostings

import (
	"context"
)

// GetSalesforceJobPostings finds JobPostings found at https://salesforce.wd1.myworkdayjobs.com/External_Career_Site
func GetSalesforceJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://salesforce.wd1.myworkdayjobs.com/External_Career_Site")
}
