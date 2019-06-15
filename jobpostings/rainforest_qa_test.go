package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetRainforestQAJobPostings(t *testing.T) {
	jobPostings, err := GetRainforestQAJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}