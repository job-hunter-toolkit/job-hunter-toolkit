# Job Hunter Toolkit

[![GitHub license](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/job-hunter-toolkit/job-hunter-toolkit/blob/master/LICENSE)
![GitHub action](https://github.com/job-hunter-toolkit/job-hunter-toolkit/workflows/CI/badge.svg)
[![go report](https://goreportcard.com/badge/github.com/job-hunter-toolkit/job-hunter-toolkit)](https://github.com/job-hunter-toolkit/job-hunter-toolkit/pulls)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](https://github.com/job-hunter-toolkit/job-hunter-toolkit/pulls)

The job hunter's toolkit. Helps search job postings for hundreds of companies.

> 👩🏽‍💻 To get started, [check out the interactive tutorial](https://www.katacoda.com/picat/scenarios/job-hunter-toolkit).

## Install

```console
$ go get -u -v github.com/job-hunter-toolkit/job-hunter-toolkit
...
```

## Help

```console
$ job-hunter-toolkit help
Usage:
  job-hunter-toolkit [command]

Available Commands:
  help         Help about any command
  job-postings Find job postings from various companies
  job-sources  List the various companies available from job-postings

Flags:
  -h, --help   help for job-hunter-toolkit

Use "job-hunter-toolkit [command] --help" for more information about a command.
```

## Job Postings

```console
$ job-hunter-toolkit job-postings
company: ... title: ... location: ... url: ...
company: ... title: ... location: ... url: ...
...
```

Output as newline separated JSON:

```console
$ job-hunter-toolkit job-postings --json
{"company":"...","title":"...","location":"...","url":"..."}
{"company":"...","title":"...","location":"...","url":"..."}
...
```

Output as CSV (with no headers):

```console
$ job-hunter-toolkit job-postings --csv
company,title,location,url
company,title,location,url
company,title,location,url
...
```

## Total Job Postings Over Time

<img alt="Total Job Postings Over Time" src="https://raw.githubusercontent.com/job-hunter-toolkit/job-hunter-toolkit/master/jobs_record.png" height="500" />
