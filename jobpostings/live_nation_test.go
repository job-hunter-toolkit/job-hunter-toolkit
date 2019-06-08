package jobpostings

import "testing"
import "context"
import "fmt"

func TestGetLiveNationJobPostings(t *testing.T) {
	jobPostings, err := GetLiveNationJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
