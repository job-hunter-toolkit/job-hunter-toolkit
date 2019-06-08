package jabba

import "testing"
import "context"
import "fmt"

func TestGet3MJobPostings(t *testing.T) {
	jobPostings, err := Get3MJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}