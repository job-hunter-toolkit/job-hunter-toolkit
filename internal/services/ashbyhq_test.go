package services

import (
	"slices"
	"testing"
)

func TestAshbyHQ(t *testing.T) {
	testSingle(t, "openai", AshbyHQ)
}

func TestAshbyHQ_all(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	testMultipleParallel(t, slices.Values(AshbyHQCompanies), AshbyHQ)
}
