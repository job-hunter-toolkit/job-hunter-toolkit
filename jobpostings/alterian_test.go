package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetAlterianJobPostings(t *testing.T) {
	jobPostings, err := GetAlterianJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}