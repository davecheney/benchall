# Benchall

A small test harness for benchmarking the compile speed improvements from Go 1.9 to tip as a standard `testing.B`.

## Prerequisites

`benchall.bash` requires Go 1.9 and Go built from tip to be installed. The respective `go` commands should be in your path as such
```
% ls -al $(which go1.9 go.tip)
lrwxrwxrwx 1 dfc dfc 22 Sep  7 21:29 /home/dfc/bin/go1.9 -> /home/dfc/go1.9/bin/go
lrwxrwxrwx 1 dfc dfc 12 Mar 15  2017 /home/dfc/bin/go.tip -> ../go/bin/go
```

`benchall.bash` also requires `benchstat` for a comparison of the final numbers
```
% go get -u golang.org/x/perf/cmd/benchstat
```
`benchall.bash` will attempt to check that all the prerequisites are installed.
## Execution
`benchall.bash` runs 10 rounds of each benchmark, twice with Go 1.9 and twice with tip. Only the second pass of each compiler is kept. This is intended to avoid the effects of processor scaling.
```
% bash benchall.bash
```
Takes between 30 and 45 minutes.

At the end of the run `benchall.bash` will run `benchstat` to compare the results. If you loose that output you can run `benchstat` directly without re-running the benchmarks.
```
% benchstat go1.9.txt go.tip.txt 
name    old time/op  new time/op  delta
Juju-4   65.5s ± 1%   63.4s ± 1%  -3.21%  (p=0.000 n=20+20)
Kube-4   47.6s ± 2%   46.7s ± 1%  -1.72%  (p=0.000 n=18+20)
Gogs-4   14.7s ± 2%   14.3s ± 2%  -3.03%  (p=0.000 n=20+20)

