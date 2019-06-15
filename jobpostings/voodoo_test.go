package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetVoodooJobPostings(t *testing.T) {
	jobPostings, err := GetVoodooJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}