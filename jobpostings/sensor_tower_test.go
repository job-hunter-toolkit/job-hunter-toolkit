package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSensorTowerJobPostings(t *testing.T) {
	t.Parallel()

	jobPostings, err := GetSensorTowerJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
