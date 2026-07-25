package services

import (
	"slices"
	"testing"
)

func TestGem(t *testing.T) {
	testSingle(t, "bluesky", Gem)
}

func TestGem_all(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	testMultipleParallel(t, slices.Values(GemCompanies), Gem)
}
