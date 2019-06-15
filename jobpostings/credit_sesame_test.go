package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetCreditSesameJobPostings(t *testing.T) {
	jobPostings, err := GetCreditSesameJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}