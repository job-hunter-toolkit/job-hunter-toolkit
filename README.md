# JHT

The job hunter's toolkit.

## Work In Progress

But contributions, issues and questions are welcome!

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
