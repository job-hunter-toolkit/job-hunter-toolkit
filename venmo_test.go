package jabba

import "testing"
import "context"
import "fmt"

func TestGetVenmoJobPostings(t *testing.T) {
	jobPostings, err := GetVenmoJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}