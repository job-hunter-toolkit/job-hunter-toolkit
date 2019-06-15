package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSkydioJobPostings(t *testing.T) {
	jobPostings, err := GetSkydioJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}