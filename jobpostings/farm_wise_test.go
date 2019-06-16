package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetFarmWiseJobPostings(t *testing.T) {
	jobPostings, err := GetFarmWiseJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
