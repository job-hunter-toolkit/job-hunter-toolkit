// Command probe dumps postings for arbitrary platform/key sources as NDJSON.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
)

type row struct {
	Platform string `json:"platform"`
	Key      string `json:"key"`
	Company  string `json:"company"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Location string `json:"location"`
	ReqID    string `json:"req"`
	ExtID    string `json:"ext"`
}

func main() {
	// args: platform/key platform/key ...
	want := map[string]bool{}
	for _, a := range os.Args[1:] {
		want[a] = true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()

	client := httpx.NewClient()
	enc := json.NewEncoder(os.Stdout)

	for _, src := range services.Builtin {
		id := src.Platform + "/" + src.Key
		if !want[id] {
			continue
		}
		var n, errs int
		start := time.Now()
		for p, err := range src.Jobs(ctx, client) {
			if err != nil {
				errs++
				if errs < 3 {
					fmt.Fprintln(os.Stderr, "error:", id, err)
				}
				continue
			}
			n++
			_ = enc.Encode(row{src.Platform, src.Key, p.Company, p.URL, p.Title, p.Location, p.RequisitionID, p.ExternalID})
		}
		fmt.Fprintf(os.Stderr, "%s: %d postings, %d errors, %s\n", id, n, errs, time.Since(start).Round(time.Second))
		delete(want, id)
	}
	for id := range want {
		fmt.Fprintln(os.Stderr, "NOT FOUND:", id, strings.Repeat("", 0))
	}
}
