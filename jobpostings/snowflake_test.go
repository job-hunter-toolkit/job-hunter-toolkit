package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSnowflakeJobPostings(t *testing.T) {
	jobPostings, err := GetSnowflakeJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}