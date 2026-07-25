package services

import (
	"slices"
	"testing"
)

func TestJibe(t *testing.T) {
	testSingle(t, "bjc", Jibe)
}

func TestJibe_all(t *testing.T) {
	testMultipleParallel(t, slices.Values(JibeCompanies), Jibe)
}
