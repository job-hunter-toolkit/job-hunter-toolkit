package engine

import (
	"fmt"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/corpus"
)

// BootstrapItem binds a rendered item to its immutable table row. SourceRow is
// meaningful only together with the generation content digest: compaction may
// move a row in a later generation even when the posting itself is unchanged.
type BootstrapItem struct {
	SourceRow uint32 `json:"source_row"`
	Item      Item   `json:"item"`
}

// BootstrapPage is the deterministic default page used to build an additive
// early-paint artifact. It is intentionally a native build-time surface, not a
// Wasm operation and not a partial-search API.
type BootstrapPage struct {
	Matched   int             `json:"matched"`
	CountUnit string          `json:"count_unit"`
	States    map[string]int  `json:"states"`
	Items     []BootstrapItem `json:"items"`
}

// DefaultBootstrapPage returns the same open-and-stale, newest-first page as an
// empty SearchRequest, with immutable source-row bindings added for artifact
// verification. Keep the parity test beside this method: an early page must be
// removed rather than allowed to drift from full search semantics.
func (e *Engine) DefaultBootstrapPage(limit int) (BootstrapPage, error) {
	if e.rows == nil {
		return BootstrapPage{}, fmt.Errorf("engine: DefaultBootstrapPage before Load")
	}
	if limit <= 0 || limit > MaxLimit {
		limit = MaxLimit
	}

	page := BootstrapPage{
		CountUnit: "rows",
		States:    map[string]int{},
		Items:     make([]BootstrapItem, 0, limit),
	}
	for _, sourceRow := range e.order {
		row := &e.rows[sourceRow]
		if row.state != corpus.StateOpen && row.state != corpus.StateStale {
			continue
		}

		page.Matched++
		page.States[row.state.String()]++
		if len(page.Items) < limit {
			page.Items = append(page.Items, BootstrapItem{
				SourceRow: sourceRow,
				Item:      e.item(int(sourceRow)),
			})
		}
	}

	return page, nil
}
