package internal

import "github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"

// The posting record and its vocabulary now live in the public
// [github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting] package, so that
// something outside this repository can name the type this project is built
// around. Everything below is an alias or a one-line forward to it: there is
// exactly one definition of each of these, and no conversion happens at the
// boundary.
//
// These shims exist so the crawler, the adapters and the CLI keep compiling
// unchanged while the extraction proceeds package by package. They are expected
// to be deleted once the callers import the public packages directly; see
// docs/design/package-taxonomy.md §9 and docs/design/public-api-extraction.md.

// JobPosting is [jobposting.JobPosting].
type JobPosting = jobposting.JobPosting

// PostingSource is [jobposting.PostingSource].
type PostingSource = jobposting.PostingSource

// EmploymentType is [jobposting.EmploymentType].
type EmploymentType = jobposting.EmploymentType

// The canonical employment types, from [jobposting].
const (
	EmploymentTypeUnknown    = jobposting.EmploymentTypeUnknown
	EmploymentTypeFullTime   = jobposting.EmploymentTypeFullTime
	EmploymentTypePartTime   = jobposting.EmploymentTypePartTime
	EmploymentTypeContract   = jobposting.EmploymentTypeContract
	EmploymentTypeInternship = jobposting.EmploymentTypeInternship
	EmploymentTypeTemporary  = jobposting.EmploymentTypeTemporary
	EmploymentTypeVolunteer  = jobposting.EmploymentTypeVolunteer
)

// WorkplaceType is [jobposting.WorkplaceType].
type WorkplaceType = jobposting.WorkplaceType

// The canonical workplace types, from [jobposting].
const (
	WorkplaceTypeUnknown = jobposting.WorkplaceTypeUnknown
	WorkplaceTypeRemote  = jobposting.WorkplaceTypeRemote
	WorkplaceTypeHybrid  = jobposting.WorkplaceTypeHybrid
	WorkplaceTypeOnsite  = jobposting.WorkplaceTypeOnsite
)

// EmploymentTypeValues calls [jobposting.EmploymentTypeValues].
func EmploymentTypeValues() []EmploymentType { return jobposting.EmploymentTypeValues() }

// WorkplaceTypeValues calls [jobposting.WorkplaceTypeValues].
func WorkplaceTypeValues() []WorkplaceType { return jobposting.WorkplaceTypeValues() }

// NormalizeEmploymentType calls [jobposting.NormalizeEmploymentType].
func NormalizeEmploymentType(raw string) (EmploymentType, bool) {
	return jobposting.NormalizeEmploymentType(raw)
}

// NormalizeWorkplaceType calls [jobposting.NormalizeWorkplaceType].
func NormalizeWorkplaceType(raw string) (WorkplaceType, bool) {
	return jobposting.NormalizeWorkplaceType(raw)
}

// Period is [jobposting.Period].
type Period = jobposting.Period

// Pay periods, from [jobposting].
const (
	PeriodUnknown = jobposting.PeriodUnknown
	PeriodHour    = jobposting.PeriodHour
	PeriodDay     = jobposting.PeriodDay
	PeriodWeek    = jobposting.PeriodWeek
	PeriodMonth   = jobposting.PeriodMonth
	PeriodYear    = jobposting.PeriodYear
)

// Compensation is [jobposting.Compensation].
type Compensation = jobposting.Compensation

// Provenance is [jobposting.Provenance].
type Provenance = jobposting.Provenance

// Provenance values, from [jobposting], in descending order of trustworthiness.
const (
	ProvenanceEmployer    = jobposting.ProvenanceEmployer
	ProvenanceStructured  = jobposting.ProvenanceStructured
	ProvenanceDescription = jobposting.ProvenanceDescription
)
