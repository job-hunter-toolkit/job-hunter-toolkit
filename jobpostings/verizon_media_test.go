package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetVerizonMediaJobPostings(t *testing.T) {
	jobPostings, err := GetVerizonMediaJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
