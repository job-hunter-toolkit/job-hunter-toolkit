package engine_test

import (
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/web/engine"
	"github.com/shoenig/test/must"
)

func TestDefaultBootstrapPageHasExactSearchParity(t *testing.T) {
	e := open(t)
	search, err := e.Search(engine.SearchRequest{})
	must.NoError(t, err)
	page, err := e.DefaultBootstrapPage(engine.MaxLimit)
	must.NoError(t, err)

	must.Eq(t, search.Matched, page.Matched)
	must.Eq(t, search.CountUnit, page.CountUnit)
	must.Eq(t, search.States, page.States)
	must.Len(t, len(search.Items), page.Items)
	for i := range search.Items {
		must.Eq(t, search.Items[i], page.Items[i].Item)
	}
}

func TestDefaultBootstrapPageRequiresLoadedEngine(t *testing.T) {
	var e engine.Engine
	_, err := e.DefaultBootstrapPage(100)
	must.ErrorContains(t, err, "before Load")
}
