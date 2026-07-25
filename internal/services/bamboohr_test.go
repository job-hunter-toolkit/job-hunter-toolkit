package services

import (
	"slices"
	"testing"
)

func TestBambooHR(t *testing.T) {
	testSingle(t, "zerofox", BambooHR)
}

func TestBambooHR_all(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	testMultipleParallel(t, slices.Values(BambooHRCompanies), BambooHR)
}
