package jobpostings

import "testing"
import "context"
import "fmt"

func TestGetTripAdvisorJobPostings(t *testing.T) {
	jobPostings, err := GetTripAdvisorJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
