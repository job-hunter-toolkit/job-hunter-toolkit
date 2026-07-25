package services

import (
	"slices"
	"testing"
)

func TestJobvite(t *testing.T) {
	testSingle(t, "splunk-careers", Jobvite)
}

func TestJobvite_all(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	testMultipleParallel(t, slices.Values(JobviteCompanies), Jobvite)
}
