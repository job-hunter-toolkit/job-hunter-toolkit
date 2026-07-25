package services

import (
	"slices"
	"testing"
)

func TestLever(t *testing.T) {
	testSingle(t, "plaid", Lever)
}

func TestLever_all(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	testMultipleParallel(t, slices.Values(LeverCompanies), Lever)
}
