package jabba

import "testing"
import "context"
import "fmt"

func TestGet21stCenturyFoxJobPostings(t *testing.T) {
	jobPostings, err := Get21stCenturyFoxJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}