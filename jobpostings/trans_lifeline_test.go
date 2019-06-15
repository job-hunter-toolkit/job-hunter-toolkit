package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetTransLifelineJobPostings(t *testing.T) {
	jobPostings, err := GetTransLifelineJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}