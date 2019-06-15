package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetGuerrillaJobPostings(t *testing.T) {
	jobPostings, err := GetGuerrillaJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
