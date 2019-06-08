package jabba

import "testing"
import "context"
import "fmt"

func TestGetImageworksJobPostings(t *testing.T) {
	jobPostings, err := GetImageworksJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}