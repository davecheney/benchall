#!/bin/bash

set -e

command -v go1.9 >/dev/null 2>&1 || {
	echo 'Please install Go 1.9.x and create a symlink to the Go 1.9 bin/go tool in your $PATH as go1.9'
	exit 1
}

command -v go.tip >/dev/null 2>&1 || {
	echo 'Please install the development version of Go and create a symlink to the bin/go tool in your $PATH as go.tip'
	exit 1
}

command -v benchstat >/dev/null 2>&1 || {
	echo 'Please install benchstat; go get -u golang.org/x/perf/cmd/benchstat'
	exit 1
}

for f in go1.9 go.tip go1.9 go.tip ; do
	${f} test -bench=. -count=10 -timeout=60m | tee ${f}.txt
done

benchstat go1.9.txt go.tip.txt
