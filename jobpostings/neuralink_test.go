package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetNeuralinkJobPostings(t *testing.T) {
	jobPostings, err := GetNeuralinkJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}