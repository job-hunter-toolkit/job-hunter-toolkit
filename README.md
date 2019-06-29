# Job Hunter Toolkit

[![GitHub license](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/job-hunter-toolkit/job-hunter-toolkit/blob/master/LICENSE)
[![CircleCI Status](https://circleci.com/gh/job-hunter-toolkit/job-hunter-toolkit.svg?style=shield&circle-token=:circle-token)](https://circleci.com/gh/job-hunter-toolkit/job-hunter-toolkit)
[![go report](https://goreportcard.com/badge/github.com/job-hunter-toolkit/job-hunter-toolkit)](https://github.com/job-hunter-toolkit/job-hunter-toolkit/pulls)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](https://github.com/job-hunter-toolkit/job-hunter-toolkit/pulls)

The job hunter's toolkit. Helps search job postings for hundreds of companies.

> 👩🏽‍💻 To get started, [check out the interactive tutorial](https://www.katacoda.com/picat/scenarios/job-hunter-toolkit).

## Install

```console
$ go get -u github.com/job-hunter-toolkit/job-hunter-toolkit
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
title: ... location: ... url: ...
title: ... location: ... url: ...
title: ... location: ... url: ...
...
```

Output as newline separated JSON:

```console
$ job-hunter-toolkit job-postings --json
{"title":"...","location":"...","url":"..."}
{"title":"...","location":"...","url":"..."}
{"title":"...","location":"...","url":"..."}
...
```

Output as CSV (with no headers):

```console
$ job-hunter-toolkit job-postings --csv
title,location,url
title,location,url
title,location,url
...
```
