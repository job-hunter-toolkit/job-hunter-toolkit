package services

import (
	"slices"
	"testing"
)

func TestWorkday(t *testing.T) {
	testSingle(t, "https://comcast.wd5.myworkdayjobs.com/Comcast_Careers", Workday)
}

func TestWorkday_all(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	testMultipleParallel(t, slices.Values(WorkdayCompanyURLs), Workday)
}
