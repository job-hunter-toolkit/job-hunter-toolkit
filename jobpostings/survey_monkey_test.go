package jobpostings

import "testing"
import "context"
import "fmt"

func TestGetSurveyMonkeyJobPostings(t *testing.T) {
	jobPostings, err := GetSurveyMonkeyJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
