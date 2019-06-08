package jobpostings

import "testing"
import "context"
import "fmt"

func TestGetElasticJobPostings(t *testing.T) {
	jobPostings, err := GetElasticJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location, "url:", jobPosting.URL)
	}
}
