package jobpostings

import (
	"context"
)

// GetProofpointJobPostings finds JobPostings using https://proofpoint.wd5.myworkdayjobs.com/ProofpointCareers
func GetProofpointJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://proofpoint.wd5.myworkdayjobs.com/ProofpointCareers")
}
