package tests_test

import (
	"fmt"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/tests"
)

type testT struct {
	errors []string
}

func (t *testT) Helper() {}
func (t *testT) Fatalf(format string, args ...any) {
	t.errors = append(t.errors, fmt.Sprintf(format, args...))
}

func TestCheckJobPosting(t *testing.T) {
	jobTests := []struct {
		name  string
		job   *internal.JobPosting
		valid bool
	}{
		{
			name: "valid job posting",
			job: &internal.JobPosting{
				URL:      "https://example.com/job/123",
				Title:    "Software Engineer",
				Company:  "Example Corp",
				Location: "Remote",
			},
			valid: true,
		},
		{
			name: "invalid job posting with empty URL",
			job: &internal.JobPosting{
				URL:      "",
				Title:    "Software Engineer",
				Company:  "Example Corp",
				Location: "Remote",
			},
			valid: false,
		},
		{
			name: "invalid job posting with empty company",
			job: &internal.JobPosting{
				URL:      "https://example.com/job/123",
				Title:    "Software Engineer",
				Company:  "",
				Location: "Remote",
			},
			valid: false,
		},
		// {
		// 	name: "invalid job posting with empty title",
		// 	job: &internal.JobPosting{
		// 		URL:      "https://example.com/job/123",
		// 		Title:    "",
		// 		Company:  "Example Corp",
		// 		Location: "Remote",
		// 	},
		// 	valid: false,
		// },
		// {
		// 	name: "invalid job posting with empty location",
		// 	job: &internal.JobPosting{
		// 		URL:      "https://example.com/job/123",
		// 		Title:    "Software Engineer",
		// 		Company:  "Example Corp",
		// 		Location: "",
		// 	},
		// 	valid: false,
		// },
	}

	for _, jobTest := range jobTests {
		t.Run(jobTest.name, func(t *testing.T) {
			jobT := &testT{}

			tests.CheckJobPosting(jobT, jobTest.job)

			switch {
			case !jobTest.valid && len(jobT.errors) == 0:
				t.Errorf("expected errors, got none")
			case jobTest.valid && len(jobT.errors) > 0:
				t.Errorf("expected no errors, got: %v", jobT.errors)
			}
		})
	}
}
