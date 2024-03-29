package jobpostings

import (
	"context"
)

// GetLiveNationJobPostings finds JobPostings using https://livenation.wd1.myworkdayjobs.com/LNExternalSite
func GetLiveNationJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://livenation.wd1.myworkdayjobs.com/LNExternalSite")
}
