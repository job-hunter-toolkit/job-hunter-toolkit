package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetPsiKickJobPostings(t *testing.T) {
	jobPostings, err := GetPsiKickJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
