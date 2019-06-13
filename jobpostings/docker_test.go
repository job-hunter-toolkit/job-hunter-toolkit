package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetDockerJobPostings(t *testing.T) {
	jobPostings, err := GetDockerJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}