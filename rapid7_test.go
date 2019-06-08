package jabba

import "testing"
import "context"
import "fmt"

func TestGetRapid7JobPostings(t *testing.T) {
	jobPostings, err := GetRapid7JobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}