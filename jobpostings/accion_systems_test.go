package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetAccionSystemsJobPostings(t *testing.T) {
	jobPostings, err := GetAccionSystemsJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}