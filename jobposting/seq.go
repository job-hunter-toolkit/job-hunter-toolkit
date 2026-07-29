package jobposting

import "iter"

// Seq is a sequence of job postings, or an error if one occurs
// while fetching the postings. The first error might not be the end
// of the sequence, depending on how the jobs are sourced.
type Seq = iter.Seq2[*JobPosting, error]

// Dedupe returns the postings from jobs with duplicates removed.
//
// Duplicates are common in practice: a company can appear under more than one
// ATS slug, and boards sometimes list the same role in several locations.
// Postings are considered the same when they share a URL, since that is the
// identity a job seeker actually cares about.
//
// Errors pass through unchanged and are never deduplicated.
func Dedupe(jobs Seq) Seq {
	return func(yield func(*JobPosting, error) bool) {
		seen := make(map[string]struct{})

		for job, err := range jobs {
			if err != nil {
				if !yield(nil, err) {
					return
				}
				continue
			}

			if job == nil {
				continue
			}

			key := job.URL
			if key == "" {
				// Without a URL there is no stable identity, so fall back to the
				// posting's descriptive fields.
				key = job.Company + "\x00" + job.Title + "\x00" + job.Location
			}

			if _, ok := seen[key]; ok {
				continue
			}

			seen[key] = struct{}{}

			if !yield(job, nil) {
				return
			}
		}
	}
}
