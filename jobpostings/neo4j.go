package jobpostings

import (
	"context"
)

// GetNeo4jJobPostings finds JobPostings found at https://jobs.lever.co/neo4j
func GetNeo4jJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "neo4j")
}
