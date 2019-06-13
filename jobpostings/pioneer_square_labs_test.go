package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetPioneerSquareLabsJobPostings(t *testing.T) {
	jobPostings, err := GetPioneerSquareLabsJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
