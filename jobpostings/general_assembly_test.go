package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetGeneralAssemblyJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := GetGeneralAssemblyJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
