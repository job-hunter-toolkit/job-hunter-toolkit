package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetProjekt202JobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := GetProjekt202JobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
