package services

import (
	"slices"
	"testing"
)

func TestGreenhouse(t *testing.T) {
	testSingle(t, "tailscale", Greenhouse)
}

func TestGreenhouse_all(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	testMultipleParallel(t, slices.Values(GreenhouseCompanies), Greenhouse)
}
