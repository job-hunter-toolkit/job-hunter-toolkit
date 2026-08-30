package engine

import (
	"testing"
	"unsafe"

	"github.com/shoenig/test/must"
)

// Generation 11 is the incident corpus: 2,005,791 rows. A native load of its
// exact published files measured the non-record projection at 479 MiB and a
// 210 MiB allocator/decoding high-water margin. This model runs hermetically in
// CI while the full corpus stays on its publication branch.
func TestGeneration11MobileMemoryBudget(t *testing.T) {
	const (
		rows               = 2_005_791
		nonRecordBytes     = 479 * 1024 * 1024
		peakWorkingBytes   = 210 * 1024 * 1024
		residentBudget     = 576 * 1024 * 1024
		linearMemoryBudget = 768 * 1024 * 1024
	)

	recordBytes := int(unsafe.Sizeof(record{}))
	resident := nonRecordBytes + rows*recordBytes
	peak := resident + peakWorkingBytes

	must.LessEq(t, 24, recordBytes)
	must.LessEq(t, residentBudget, resident)
	must.LessEq(t, linearMemoryBudget, peak)
}

// WebMCP calls the existing Engine methods through the existing worker. This
// structural budget catches accidentally adding a second URL map, row slice,
// or other per-generation index to Engine in a future tool implementation.
func TestWebMCPDoesNotAddASecondGeneration11Index(t *testing.T) {
	// This is the measured amd64 header before WebMCP. Dynamic column and row
	// storage is covered by TestGeneration11MobileMemoryBudget above.
	must.Eq(t, uintptr(1376), unsafe.Sizeof(Engine{}))
}
