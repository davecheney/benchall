/*
Copyright 2015 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"fmt"
	"strings"
	"time"

	"k8s.io/kubernetes/pkg/api"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/util"
	"k8s.io/kubernetes/pkg/util/sets"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	// Interval to poll /stats/container on a node
	containerStatsPollingPeriod = 3 * time.Second
	// The monitoring time for one test.
	monitoringTime = 20 * time.Minute
	// The periodic reporting period.
	reportingPeriod = 5 * time.Minute
)

type resourceTest struct {
	podsPerNode int
	cpuLimits   containersCPUSummary
	memLimits   resourceUsagePerContainer
}

func logPodsOnNodes(c *client.Client, nodeNames []string) {
	for _, n := range nodeNames {
		podList, err := GetKubeletPods(c, n)
		if err != nil {
			Logf("Unable to retrieve kubelet pods for node %v", n)
			continue
		}
		Logf("%d pods are running on node %v", len(podList.Items), n)
	}
}

func runResourceTrackingTest(framework *Framework, podsPerNode int, nodeNames sets.String, rm *resourceMonitor,
	expectedCPU map[string]map[float64]float64, expectedMemory resourceUsagePerContainer) {
	numNodes := nodeNames.Len()
	totalPods := podsPerNode * numNodes
	By(fmt.Sprintf("Creating a RC of %d pods and wait until all pods of this RC are running", totalPods))
	rcName := fmt.Sprintf("resource%d-%s", totalPods, string(util.NewUUID()))

	// TODO: Use a more realistic workload
	Expect(RunRC(RCConfig{
		Client:    framework.Client,
		Name:      rcName,
		Namespace: framework.Namespace.Name,
		Image:     "gcr.io/google_containers/pause:2.0",
		Replicas:  totalPods,
	})).NotTo(HaveOccurred())

	// Log once and flush the stats.
	rm.LogLatest()
	rm.Reset()

	By("Start monitoring resource usage")
	// Periodically dump the cpu summary until the deadline is met.
	// Note that without calling resourceMonitor.Reset(), the stats
	// would occupy increasingly more memory. This should be fine
	// for the current test duration, but we should reclaim the
	// entries if we plan to monitor longer (e.g., 8 hours).
	deadline := time.Now().Add(monitoringTime)
	for time.Now().Before(deadline) {
		timeLeft := deadline.Sub(time.Now())
		Logf("Still running...%v left", timeLeft)
		if timeLeft < reportingPeriod {
			time.Sleep(timeLeft)
		} else {
			time.Sleep(reportingPeriod)
		}
		logPodsOnNodes(framework.Client, nodeNames.List())
	}

	By("Reporting overall resource usage")
	logPodsOnNodes(framework.Client, nodeNames.List())
	usageSummary, err := rm.GetLatest()
	Expect(err).NotTo(HaveOccurred())
	Logf("%s", rm.FormatResourceUsage(usageSummary))
	verifyMemoryLimits(framework.Client, expectedMemory, usageSummary)

	cpuSummary := rm.GetCPUSummary()
	Logf("%s", rm.FormatCPUSummary(cpuSummary))
	verifyCPULimits(expectedCPU, cpuSummary)

	By("Deleting the RC")
	DeleteRC(framework.Client, framework.Namespace.Name, rcName)
}

func verifyMemoryLimits(c *client.Client, expected resourceUsagePerContainer, actual resourceUsagePerNode) {
	if expected == nil {
		return
	}
	var errList []string
	for nodeName, nodeSummary := range actual {
		var nodeErrs []string
		for cName, expectedResult := range expected {
			container, ok := nodeSummary[cName]
			if !ok {
				nodeErrs = append(nodeErrs, fmt.Sprintf("container %q: missing", cName))
				continue
			}

			expectedValue := expectedResult.MemoryRSSInBytes
			actualValue := container.MemoryRSSInBytes
			if expectedValue != 0 && actualValue > expectedValue {
				nodeErrs = append(nodeErrs, fmt.Sprintf("container %q: expected RSS memory (MB) < %d; got %d",
					cName, expectedValue, actualValue))
			}
		}
		if len(nodeErrs) > 0 {
			errList = append(errList, fmt.Sprintf("node %v:\n %s", nodeName, strings.Join(nodeErrs, ", ")))
			heapStats, err := getKubeletHeapStats(c, nodeName)
			if err != nil {
				Logf("Unable to get heap stats from %q", nodeName)
			} else {
				Logf("Heap stats on %q\n:%v", nodeName, heapStats)
			}
		}
	}
	if len(errList) > 0 {
		Failf("Memory usage exceeding limits:\n %s", strings.Join(errList, "\n"))
	}
}

func verifyCPULimits(expected containersCPUSummary, actual nodesCPUSummary) {
	if expected == nil {
		return
	}
	var errList []string
	for nodeName, perNodeSummary := range actual {
		var nodeErrs []string
		for cName, expectedResult := range expected {
			perContainerSummary, ok := perNodeSummary[cName]
			if !ok {
				nodeErrs = append(nodeErrs, fmt.Sprintf("container %q: missing", cName))
				continue
			}
			for p, expectedValue := range expectedResult {
				actualValue, ok := perContainerSummary[p]
				if !ok {
					nodeErrs = append(nodeErrs, fmt.Sprintf("container %q: missing percentile %v", cName, p))
					continue
				}
				if actualValue > expectedValue {
					nodeErrs = append(nodeErrs, fmt.Sprintf("container %q: expected %.0fth%% usage < %.3f; got %.3f",
						cName, p*100, expectedValue, actualValue))
				}
			}
		}
		if len(nodeErrs) > 0 {
			errList = append(errList, fmt.Sprintf("node %v:\n %s", nodeName, strings.Join(nodeErrs, ", ")))
		}
	}
	if len(errList) > 0 {
		Failf("CPU usage exceeding limits:\n %s", strings.Join(errList, "\n"))
	}
}

// Slow by design (1 hour)
var _ = Describe("Kubelet [Serial] [Slow]", func() {
	var nodeNames sets.String
	framework := NewDefaultFramework("kubelet-perf")
	var rm *resourceMonitor

	BeforeEach(func() {
		// It should be OK to list unschedulable Nodes here.
		nodes, err := framework.Client.Nodes().List(api.ListOptions{})
		expectNoError(err)
		nodeNames = sets.NewString()
		for _, node := range nodes.Items {
			nodeNames.Insert(node.Name)
		}
		rm = newResourceMonitor(framework.Client, targetContainers(), containerStatsPollingPeriod)
		rm.Start()
	})

	AfterEach(func() {
		rm.Stop()
	})
	Describe("regular resource usage tracking", func() {
		// We assume that the scheduler will make reasonable scheduling choices
		// and assign ~N pods on the node.
		// Although we want to track N pods per node, there are N + add-on pods
		// in the cluster. The cluster add-on pods can be distributed unevenly
		// among the nodes because they are created during the cluster
		// initialization. This *noise* is obvious when N is small. We
		// deliberately set higher resource usage limits to account for the
		// noise.
		rTests := []resourceTest{
			{
				podsPerNode: 0,
				cpuLimits: containersCPUSummary{
					"/kubelet":       {0.50: 0.06, 0.95: 0.08},
					"/docker-daemon": {0.50: 0.05, 0.95: 0.06},
				},
				// We set the memory limits generously because the distribution
				// of the addon pods affect the memory usage on each node.
				memLimits: resourceUsagePerContainer{
					"/kubelet":       &containerResourceUsage{MemoryRSSInBytes: 70 * 1024 * 1024},
					"/docker-daemon": &containerResourceUsage{MemoryRSSInBytes: 85 * 1024 * 1024},
				},
			},
			{
				podsPerNode: 35,
				cpuLimits: containersCPUSummary{
					"/kubelet":       {0.50: 0.12, 0.95: 0.14},
					"/docker-daemon": {0.50: 0.06, 0.95: 0.08},
				},
				// We set the memory limits generously because the distribution
				// of the addon pods affect the memory usage on each node.
				memLimits: resourceUsagePerContainer{
					"/kubelet":       &containerResourceUsage{MemoryRSSInBytes: 75 * 1024 * 1024},
					"/docker-daemon": &containerResourceUsage{MemoryRSSInBytes: 100 * 1024 * 1024},
				},
			},
			{
				// TODO(yujuhong): Set the limits after collecting enough data.
				podsPerNode: 100,
			},
		}
		for _, testArg := range rTests {
			itArg := testArg
			podsPerNode := itArg.podsPerNode
			name := fmt.Sprintf(
				"for %d pods per node over %v", podsPerNode, monitoringTime)
			It(name, func() {
				runResourceTrackingTest(framework, podsPerNode, nodeNames, rm, itArg.cpuLimits, itArg.memLimits)
			})
		}
	})
	Describe("experimental resource usage tracking [Feature:ExperimentalResourceUsageTracking]", func() {
		density := []int{100}
		for i := range density {
			podsPerNode := density[i]
			name := fmt.Sprintf(
				"for %d pods per node over %v", podsPerNode, monitoringTime)
			It(name, func() {
				runResourceTrackingTest(framework, podsPerNode, nodeNames, rm, nil, nil)
			})
		}
	})
})
