package jabba

import (
	"context"
)

// GetNvidiaJobPostings finds JobPostings using https://nvidia.wd5.myworkdayjobs.com/NVIDIAExternalCareerSite
func GetNvidiaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://nvidia.wd5.myworkdayjobs.com/NVIDIAExternalCareerSite")
}