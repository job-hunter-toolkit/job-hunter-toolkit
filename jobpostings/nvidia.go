package jobpostings

import (
	"context"
)

// GetNvidiaJobPostings finds JobPostings using https://nvidia.wd5.myworkdayjobs.com/NVIDIAExternalCareerSite
func GetNvidiaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://nvidia.wd5.myworkdayjobs.com/NVIDIAExternalCareerSite")
}
