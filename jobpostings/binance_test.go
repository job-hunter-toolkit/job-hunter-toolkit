package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetBinanceJobPostings(t *testing.T) {
	jobPostings, err := GetBinanceJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}