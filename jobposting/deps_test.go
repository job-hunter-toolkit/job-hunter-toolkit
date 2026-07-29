package jobposting_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestDependenciesAreStandardLibraryOnly asserts the rule the package doc
// states: jobposting links nothing outside the standard library, and not
// net/http.
//
// It is a real constraint rather than a stylistic one. Merely importing
// net/http cost 3.31 MB linked on linux/amd64 and 2.98 MB on js/wasm when
// docs/design/package-taxonomy.md §1.1 measured it, and this is the package
// every consumer of the project links first. It also enforces the boundary rule
// from docs/surfaces-and-extensibility.md §2: a public type must not
// transitively expose an internal one, and the cheapest way for that to happen
// is an import nobody noticed.
func TestDependenciesAreStandardLibraryOnly(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain on PATH:", err)
	}

	const self = "github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"

	out, err := exec.Command("go", "list", "-deps", self).Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}

	for _, dep := range strings.Fields(string(out)) {
		// go list -deps includes the package itself, last.
		if dep == self {
			continue
		}

		// A dot in the first path element means a module domain, so anything
		// outside the standard library, including this module's own packages.
		first, _, _ := strings.Cut(dep, "/")
		if strings.Contains(first, ".") {
			t.Errorf("jobposting must depend only on the standard library, but depends on %q", dep)
		}

		if dep == "net/http" {
			t.Error("jobposting must not depend on net/http; see the package doc")
		}
	}
}
