package services

import (
	"slices"
	"testing"
)

func TestRippling(t *testing.T) {
	testSingle(t, "chess", AshbyHQ)
}

func TestRippling_all(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	testMultipleParallel(t, slices.Values(RipplingCompanies), Rippling)
}
