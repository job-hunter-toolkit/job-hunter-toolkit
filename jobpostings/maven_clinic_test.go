package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetMavenClinicJobPostings(t *testing.T) {
	jobPostings, err := GetMavenClinicJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
