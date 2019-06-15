package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetGeckoboardJobPostings(t *testing.T) {
	jobPostings, err := GetGeckoboardJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}