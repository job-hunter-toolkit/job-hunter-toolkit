package jabba

import "testing"
import "context"
import "fmt"

func TestGetTwilioJobPostings(t *testing.T) {
	jobPostings, err := GetTwilioJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}