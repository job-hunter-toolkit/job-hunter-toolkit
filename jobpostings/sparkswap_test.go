package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSparkswapJobPostings(t *testing.T) {
	jobPostings, err := GetSparkswapJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}