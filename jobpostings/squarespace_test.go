package jobpostings

import "testing"
import "context"
import "fmt"

func TestGetSquarespaceJobPostings(t *testing.T) {
	jobPostings, err := GetSquarespaceJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
