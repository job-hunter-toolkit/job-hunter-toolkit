package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetDuoSecurityJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := GetDuoSecurityJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
