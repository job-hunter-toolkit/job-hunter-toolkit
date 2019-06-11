package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetZumeJobPostings(t *testing.T) {
	jobPostings, err := GetZumeJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
