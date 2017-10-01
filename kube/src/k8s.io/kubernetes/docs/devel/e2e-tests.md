<!-- BEGIN MUNGE: UNVERSIONED_WARNING -->

<!-- BEGIN STRIP_FOR_RELEASE -->

<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">
<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">
<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">
<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">
<img src="http://kubernetes.io/img/warning.png" alt="WARNING"
     width="25" height="25">

<h2>PLEASE NOTE: This document applies to the HEAD of the source tree</h2>

If you are using a released version of Kubernetes, you should
refer to the docs that go with that version.

<!-- TAG RELEASE_LINK, added by the munger automatically -->
<strong>
The latest release of this document can be found
[here](http://releases.k8s.io/release-1.1/docs/devel/e2e-tests.md).

Documentation for other releases can be found at
[releases.k8s.io](http://releases.k8s.io).
</strong>
--

<!-- END STRIP_FOR_RELEASE -->

<!-- END MUNGE: UNVERSIONED_WARNING -->

# End-to-End Testing in Kubernetes

## Overview

End-to-end (e2e) tests for Kubernetes provide a mechanism to test end-to-end behavior of the system, and is the last signal to ensure end user operations match developer specifications.  Although unit and integration tests should ideally provide a good signal, the reality is in a distributed system like Kubernetes it is not uncommon that a minor change may pass all unit and integration tests, but cause unforeseen changes at the system level.  e2e testing is very costly, both in time to run tests and difficulty debugging, though: it takes a long time to build, deploy, and exercise a cluster.  Thus, the primary objectives of the e2e tests are to ensure a consistent and reliable behavior of the kubernetes code base, and to catch hard-to-test bugs before users do, when unit and integration tests are insufficient.

The e2e tests in kubernetes are built atop of [Ginkgo](http://onsi.github.io/ginkgo/) and [Gomega](http://onsi.github.io/gomega/).  There are a host of features that this BDD testing framework provides, and it is recommended that the developer read the documentation prior to diving into the tests.

The purpose of *this* document is to serve as a primer for developers who are looking to execute or add tests using a local development environment.

Before writing new tests or making substantive changes to existing tests, you should also read [Writing Good e2e Tests](writing-good-e2e-tests.md)

## Building and Running the Tests

There are a variety of ways to run e2e tests, but we aim to decrease the number of ways to run e2e tests to a canonical way: `hack/e2e.go`.

You can run an end-to-end test which will bring up a master and nodes, perform some tests, and then tear everything down. Make sure you have followed the getting started steps for your chosen cloud platform (which might involve changing the `KUBERNETES_PROVIDER` environment variable to something other than "gce").

To build Kubernetes, up a cluster, run tests, and tear everything down, use:

```sh
go run hack/e2e.go -v --build --up --test --down
```

If you'd like to just perform one of these steps, here are some examples:

```sh
# Build binaries for testing
go run hack/e2e.go -v --build

# Create a fresh cluster.  Deletes a cluster first, if it exists
go run hack/e2e.go -v --up

# Test if a cluster is up.
go run hack/e2e.go -v --isup

# Push code to an existing cluster
go run hack/e2e.go -v --push

# Push to an existing cluster, or bring up a cluster if it's down.
go run hack/e2e.go -v --pushup

# Run all tests
go run hack/e2e.go -v --test

# Run tests matching the regex "\[Conformance\]" (the conformance tests)
go run hack/e2e.go -v -test --test_args="--ginkgo.focus=\[Conformance\]"

# Conversely, exclude tests that match the regex "Pods.*env"
go run hack/e2e.go -v -test --test_args="--ginkgo.focus=Pods.*env"

# Flags can be combined, and their actions will take place in this order:
# --build, --push|--up|--pushup, --test|--tests=..., --down
#
# You can also specify an alternative provider, such as 'aws'
#
# e.g.:
KUBERNETES_PROVIDER=aws go run hack/e2e.go -v --build --pushup --test --down

# -ctl can be used to quickly call kubectl against your e2e cluster. Useful for
# cleaning up after a failed test or viewing logs. Use -v to avoid suppressing
# kubectl output.
go run hack/e2e.go -v -ctl='get events'
go run hack/e2e.go -v -ctl='delete pod foobar'

# Alternately, if you have the e2e cluster up and no desire to see the event stream, you can run ginkgo-e2e.sh directly:
hack/ginkgo-e2e.sh --ginkgo.focus=\[Conformance\]
```

The tests are built into a single binary which can be run used to deploy a Kubernetes system or run tests against an already-deployed Kubernetes system.  See `go run hack/e2e.go --help` (or the flag definitions in `hack/e2e.go`) for more options, such as reusing an existing cluster.

### Cleaning up

During a run, pressing `control-C` should result in an orderly shutdown, but if something goes wrong and you still have some VMs running you can force a cleanup with this command:

```sh
go run hack/e2e.go -v --down
```

## Advanced testing

### Bringing up a cluster for testing

If you want, you may bring up a cluster in some other manner and run tests against it.  To do so, or to do other non-standard test things, you can pass arguments into Ginkgo using `--test_args` (e.g. see above).  For the purposes of brevity, we will look at a subset of the options, which are listed below:

```
-ginkgo.dryRun=false: If set, ginkgo will walk the test hierarchy without actually running anything.  Best paired with -v.
-ginkgo.failFast=false: If set, ginkgo will stop running a test suite after a failure occurs.
-ginkgo.failOnPending=false: If set, ginkgo will mark the test suite as failed if any specs are pending.
-ginkgo.focus="": If set, ginkgo will only run specs that match this regular expression.
-ginkgo.skip="": If set, ginkgo will only run specs that do not match this regular expression.
-ginkgo.trace=false: If set, default reporter prints out the full stack trace when a failure occurs
-ginkgo.v=false: If set, default reporter print out all specs as they begin.
-host="": The host, or api-server, to connect to
-kubeconfig="": Path to kubeconfig containing embedded authinfo.
-prom-push-gateway="": The URL to prometheus gateway, so that metrics can be pushed during e2es and scraped by prometheus. Typically something like 127.0.0.1:9091.
-provider="": The name of the Kubernetes provider (gce, gke, local, vagrant, etc.)
-repo-root="../../": Root directory of kubernetes repository, for finding test files.
```

Prior to running the tests, you may want to first create a simple auth file in your home directory, e.g. `$HOME/.kube/config` , with the following:

```
{
  "User": "root",
  "Password": ""
}
```

As mentioned earlier there are a host of other options that are available, but they are left to the developer.

**NOTE:** If you are running tests on a local cluster repeatedly, you may need to periodically perform some manual cleanup.

- `rm -rf /var/run/kubernetes`, clear kube generated credentials, sometimes stale permissions can cause problems.
- `sudo iptables -F`, clear ip tables rules left by the kube-proxy.

### Debugging clusters

If a cluster fails to initialize, or you'd like to better understand cluster
state to debug a failed e2e test, you can use the `cluster/log-dump.sh` script
to gather logs.

This script requires that the cluster provider supports ssh. Assuming it does,
running

```
cluster/log-dump.sh <directory>
````

will ssh to the master and all nodes
and download a variety of useful logs to the provided directory (which should
already exist).

The Google-run Jenkins builds automatically collected these logs for every
build, saving them in the `artifacts` directory uploaded to GCS.

### Local clusters

It can be much faster to iterate on a local cluster instead of a cloud-based one.  To start a local cluster, you can run:

```sh
# The PATH construction is needed because PATH is one of the special-cased
# environment variables not passed by sudo -E
sudo PATH=$PATH hack/local-up-cluster.sh
```

This will start a single-node Kubernetes cluster than runs pods using the local docker daemon.  Press Control-C to stop the cluster.

#### Testing against local clusters

In order to run an E2E test against a locally running cluster, point the tests at a custom host directly:

```sh
export KUBECONFIG=/path/to/kubeconfig
go run hack/e2e.go -v --test_args="--host=http://127.0.0.1:8080"
```

To control the tests that are run:

```sh
go run hack/e2e.go -v --test_args="--host=http://127.0.0.1:8080" --ginkgo.focus="Secrets"
```

## Kinds of tests

We are working on implementing clearer partitioning of our e2e tests to make running a known set of tests easier (#10548).  Tests can be labeled with any of the following labels, in order of increasing precedence (that is, each label listed below supersedes the previous ones):

- If a test has no labels, it is expected to run fast (under five minutes), be able to be run in parallel, and be consistent.
- `[Slow]`: If a test takes more than five minutes to run (by itself or in parallel with many other tests), it is labeled `[Slow]`.  This partition allows us to run almost all of our tests quickly in parallel, without waiting for the stragglers to finish.
- `[Serial]`: If a test cannot be run in parallel with other tests (e.g. it takes too many resources or restarts nodes), it is labeled `[Serial]`, and should be run in serial as part of a separate suite.
- `[Disruptive]`: If a test restarts components that might cause other tests to fail or break the cluster completely, it is labeled `[Disruptive]`.  Any `[Disruptive]` test is also assumed to qualify for the `[Serial]` label, but need not be labeled as both.  These tests are not run against soak clusters to avoid restarting components.
- `[Flaky]`: If a test is found to be flaky and we have decided that it's too hard to fix in the short term (e.g. it's going to take a full engineer-week), it receives the `[Flaky]` label until it is fixed.  The `[Flaky]` label should be used very sparingly, and should be accompanied with a reference to the issue for de-flaking the test, because while a test remains labeled `[Flaky]`, it is not monitored closely in CI. `[Flaky]` tests are by default not run, unless a `focus` or `skip` argument is explicitly given.
- `[Feature:.+]`: If a test has non-default requirements to run or targets some non-core functionality, and thus should not be run as part of the standard suite, it receives a `[Feature:.+]` label, e.g. `[Feature:Performance]` or `[Feature:Ingress]`.  `[Feature:.+]` tests are not run in our core suites, instead running in custom suites. If a feature is experimental or alpha and is not enabled by default due to being incomplete or potentially subject to breaking changes, it does *not* block the merge-queue, and thus should run in some separate test suites owned by the feature owner(s) (see #continuous_integration below).

### Conformance tests

Finally, `[Conformance]` tests are tests we expect to pass on **any** Kubernetes cluster.  The `[Conformance]` label does not supersede any other labels.  `[Conformance]` test policies are a work-in-progress (see #18162).

End-to-end testing, as described above, is for [development distributions](writing-a-getting-started-guide.md).  A conformance test is used on a [versioned distro](writing-a-getting-started-guide.md).  (Links WIP)

The conformance test runs a subset of the e2e-tests against a manually-created cluster.  It does not require support for up/push/down and other operations.  To run a conformance test, you need to know the IP of the master for your cluster and the authorization arguments to use.  The conformance test is intended to run against a cluster at a specific binary release of Kubernetes.  See [conformance-test.sh](http://releases.k8s.io/HEAD/hack/conformance-test.sh).

### Defining what Conformance means

It is impossible to define the entire space of Conformance tests without knowing the future, so instead, we define the compliment of conformance tests, below.

Please update this with companion PRs as necessary.

 - A conformance test cannot test cloud provider specific features (i.e. GCE monitoring, S3 Bucketing, ...)
 - A conformance test cannot rely on any particular non-standard file system permissions granted to containers or users (i.e. sharing writable host /tmp with a container)
 - A conformance test cannot rely on any binaries that are not required for the linux kernel or for a kubelet to run (i.e. git)
 - A conformance test cannot test a feature which obviously cannot be supported on a broad range of platforms (i.e. testing of multiple disk mounts, GPUs, high density)

## Continuous Integration

A quick overview of how we run e2e CI on Kubernetes.

### What is CI?

We run a battery of `e2e` tests against `HEAD` of the master branch on a continuous basis, and block merges via the [submit queue](http://submit-queue.k8s.io/) on a subset of those tests if they fail (the subset is defined in the [munger config](https://github.com/kubernetes/contrib/blob/master/mungegithub/mungers/submit-queue.go) via the `jenkins-jobs` flag; note we also block on	`kubernetes-build` and `kubernetes-test-go` jobs for build and unit and integration tests).

CI results can be found at [ci-test.k8s.io](http://ci-test.k8s.io), e.g. [ci-test.k8s.io/kubernetes-e2e-gce/10594](http://ci-test.k8s.io/kubernetes-e2e-gce/10594).

### What runs in CI?

We run all default tests (those that aren't marked `[Flaky]` or `[Feature:.+]`) against GCE and GKE.  To minimize the time from regression-to-green-run, we partition tests across different jobs:

- `kubernetes-e2e-<provider>` runs all non-`[Slow]`, non-`[Serial]`, non-`[Disruptive]`, non-`[Flaky]`, non-`[Feature:.+]` tests in parallel.
- `kubernetes-e2e-<provider>-slow` runs all `[Slow]`, non-`[Serial]`, non-`[Disruptive]`, non-`[Flaky]`, non-`[Feature:.+]` tests in parallel.
- `kubernetes-e2e-<provider>-serial` runs all `[Serial]` and `[Disruptive]`, non-`[Flaky]`, non-`[Feature:.+]` tests in serial.

We also run non-default tests if the tests exercise general-availability ("GA") features that require a special environment to run in, e.g. `kubernetes-e2e-gce-scalability` and `kubernetes-kubemark-gce`, which test for Kubernetes performance.

#### Non-default tests

Many `[Feature:.+]` tests we don't run in CI.  These tests are for features that are experimental (often in the `experimental` API), and aren't enabled by default.

### The PR-builder

We also run a battery of tests against every PR before we merge it.  These tests are equivalent to `kubernetes-gce`: it runs all non-`[Slow]`, non-`[Serial]`, non-`[Disruptive]`, non-`[Flaky]`, non-`[Feature:.+]` tests in parallel.  These tests are considered "smoke tests" to give a decent signal that the PR doesn't break most functionality.  Results for you PR can be found at [pr-test.k8s.io](http://pr-test.k8s.io), e.g. [pr-test.k8s.io/20354](http://pr-test.k8s.io/20354) for #20354.

### Adding a test to CI

As mentioned above, prior to adding a new test, it is a good idea to perform a `-ginkgo.dryRun=true` on the system, in order to see if a behavior is already being tested, or to determine if it may be possible to augment an existing set of tests for a specific use case.

If a behavior does not currently have coverage and a developer wishes to add a new e2e test, navigate to the ./test/e2e directory and create a new test using the existing suite as a guide.

TODO(#20357): Create a self-documented example which has been disabled, but can be copied to create new tests and outlines the capabilities and libraries used.

When writing a test, consult #kinds_of_tests above to determine how your test should be marked, (e.g. `[Slow]`, `[Serial]`; remember, by default we assume a test can run in parallel with other tests!).

When first adding a test it should *not* go straight into CI, because failures block ordinary development. A test should only be added to CI after is has been running in some non-CI suite long enough to establish a track record showing that the test does not fail when run against *working* software.  Note also that tests running in CI are generally running on a well-loaded cluster, so must contend for resources; see above about [kinds of tests](#kinds_of_tests).

Generally, a feature starts as `experimental`, and will be run in some suite owned by the team developing the feature.  If a feature is in beta or GA, it *should* block the merge-queue.  In moving from experimental to beta or GA, tests that are expected to pass by default should simply remove the `[Feature:.+]` label, and will be incorporated into our core suites.  If tests are not expected to pass by default, (e.g. they require a special environment such as added quota,) they should remain with the `[Feature:.+]` label, and the suites that run them should be incorporated into the [munger config](https://github.com/kubernetes/contrib/blob/master/mungegithub/mungers/submit-queue.go) via the `jenkins-jobs` flag.

Occasionally, we'll want to add tests to better exercise features that are already GA.  These tests also shouldn't go straight to CI.  They should begin by being marked as `[Flaky]` to be run outside of CI, and once a track-record for them is established, they may be promoted out of `[Flaky]`.

### Moving a test out of CI

If we have determined that a test is known-flaky and cannot be fixed in the short-term, we may move it out of CI indefinitely.  This move should be used sparingly, as it effectively means that we have no coverage of that test.  When a test if demoted, it should be marked `[Flaky]` with a comment accompanying the label with a reference to an issue opened to fix the test.

## Performance Evaluation

Another benefit of the e2e tests is the ability to create reproducible loads on the system, which can then be used to determine the responsiveness, or analyze other characteristics of the system.  For example, the density tests load the system to 30,50,100 pods per/node and measures the different characteristics of the system, such as throughput, api-latency, etc.

For a good overview of how we analyze performance data, please read the following [post](http://blog.kubernetes.io/2015/09/kubernetes-performance-measurements-and.html)

For developers who are interested in doing their own performance analysis, we recommend setting up [prometheus](http://prometheus.io/) for data collection, and using [promdash](http://prometheus.io/docs/visualization/promdash/) to visualize the data.  There also exists the option of pushing your own metrics in from the tests using a [prom-push-gateway](http://prometheus.io/docs/instrumenting/pushing/).  Containers for all of these components can be found [here](https://hub.docker.com/u/prom/).

For more accurate measurements, you may wish to set up prometheus external to kubernetes in an environment where it can access the major system components (api-server, controller-manager, scheduler).  This is especially useful when attempting to gather metrics in a load-balanced api-server environment, because all api-servers can be analyzed independently as well as collectively. On startup, configuration file is passed to prometheus that specifies the endpoints that prometheus will scrape, as well as the sampling interval.

```
#prometheus.conf
job: {
      name: "kubernetes"
      scrape_interval: "1s"
      target_group: {
		# apiserver(s)
		target: "http://localhost:8080/metrics"
		# scheduler 
		target: "http://localhost:10251/metrics"
		# controller-manager
		target: "http://localhost:10252/metrics"
      }
```

Once prometheus is scraping the kubernetes endpoints, that data can then be plotted using promdash, and alerts can be created against the assortment of metrics that kubernetes provides.

**HAPPY TESTING!**



<!-- BEGIN MUNGE: GENERATED_ANALYTICS -->
[![Analytics](https://kubernetes-site.appspot.com/UA-36037335-10/GitHub/docs/devel/e2e-tests.md?pixel)]()
<!-- END MUNGE: GENERATED_ANALYTICS -->
