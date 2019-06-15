package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetFatLlamaJobPostings(t *testing.T) {
	jobPostings, err := GetFatLlamaJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}