package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetCobaltRoboticsJobPostings(t *testing.T) {
	jobPostings, err := GetCobaltRoboticsJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}