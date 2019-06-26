package jobpostings

import (
	"context"
)

// GetBoseJobPostings finds JobPostings using https://boseallaboutme.wd1.myworkdayjobs.com/Bose_Careers/jobs
func GetBoseJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://boseallaboutme.wd1.myworkdayjobs.com/Bose_Careers/jobs")
}
