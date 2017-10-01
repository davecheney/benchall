/*
Copyright 2014 The Kubernetes Authors All rights reserved.

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

package resourcequota

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/golang-lru"

	"k8s.io/kubernetes/pkg/admission"
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/resource"
	"k8s.io/kubernetes/pkg/client/cache"
	"k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset/fake"
	"k8s.io/kubernetes/pkg/client/unversioned/testclient"
	"k8s.io/kubernetes/pkg/quota/install"
	"k8s.io/kubernetes/pkg/util/sets"
)

func getResourceList(cpu, memory string) api.ResourceList {
	res := api.ResourceList{}
	if cpu != "" {
		res[api.ResourceCPU] = resource.MustParse(cpu)
	}
	if memory != "" {
		res[api.ResourceMemory] = resource.MustParse(memory)
	}
	return res
}

func getResourceRequirements(requests, limits api.ResourceList) api.ResourceRequirements {
	res := api.ResourceRequirements{}
	res.Requests = requests
	res.Limits = limits
	return res
}

func validPod(name string, numContainers int, resources api.ResourceRequirements) *api.Pod {
	pod := &api.Pod{
		ObjectMeta: api.ObjectMeta{Name: name, Namespace: "test"},
		Spec:       api.PodSpec{},
	}
	pod.Spec.Containers = make([]api.Container, 0, numContainers)
	for i := 0; i < numContainers; i++ {
		pod.Spec.Containers = append(pod.Spec.Containers, api.Container{
			Image:     "foo:V" + strconv.Itoa(i),
			Resources: resources,
		})
	}
	return pod
}

// TestAdmissionIgnoresDelete verifies that the admission controller ignores delete operations
func TestAdmissionIgnoresDelete(t *testing.T) {
	kubeClient := fake.NewSimpleClientset()
	handler, err := NewResourceQuota(kubeClient, install.NewRegistry(kubeClient))
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}
	namespace := "default"
	err = handler.Admit(admission.NewAttributesRecord(nil, api.Kind("Pod"), namespace, "name", api.Resource("pods"), "", admission.Delete, nil))
	if err != nil {
		t.Errorf("ResourceQuota should admit all deletes: %v", err)
	}
}

// TestAdmissionIgnoresSubresources verifies that the admission controller ignores subresources
// It verifies that creation of a pod that would have exceeded quota is properly failed
// It verifies that create operations to a subresource that would have exceeded quota would succeed
func TestAdmissionIgnoresSubresources(t *testing.T) {
	resourceQuota := &api.ResourceQuota{}
	resourceQuota.Name = "quota"
	resourceQuota.Namespace = "test"
	resourceQuota.Status = api.ResourceQuotaStatus{
		Hard: api.ResourceList{},
		Used: api.ResourceList{},
	}
	resourceQuota.Status.Hard[api.ResourceMemory] = resource.MustParse("2Gi")
	resourceQuota.Status.Used[api.ResourceMemory] = resource.MustParse("1Gi")
	kubeClient := fake.NewSimpleClientset(resourceQuota)
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{"namespace": cache.MetaNamespaceIndexFunc})
	handler := &quotaAdmission{
		Handler:  admission.NewHandler(admission.Create, admission.Update),
		client:   kubeClient,
		indexer:  indexer,
		registry: install.NewRegistry(kubeClient),
	}
	handler.indexer.Add(resourceQuota)
	newPod := validPod("123", 1, getResourceRequirements(getResourceList("100m", "2Gi"), getResourceList("", "")))
	err := handler.Admit(admission.NewAttributesRecord(newPod, api.Kind("Pod"), newPod.Namespace, newPod.Name, api.Resource("pods"), "", admission.Create, nil))
	if err == nil {
		t.Errorf("Expected an error because the pod exceeded allowed quota")
	}
	err = handler.Admit(admission.NewAttributesRecord(newPod, api.Kind("Pod"), newPod.Namespace, newPod.Name, api.Resource("pods"), "subresource", admission.Create, nil))
	if err != nil {
		t.Errorf("Did not expect an error because the action went to a subresource: %v", err)
	}
}

// TestAdmitBelowQuotaLimit verifies that a pod when created has its usage reflected on the quota
func TestAdmitBelowQuotaLimit(t *testing.T) {
	resourceQuota := &api.ResourceQuota{
		ObjectMeta: api.ObjectMeta{Name: "quota", Namespace: "test", ResourceVersion: "124"},
		Status: api.ResourceQuotaStatus{
			Hard: api.ResourceList{
				api.ResourceCPU:    resource.MustParse("3"),
				api.ResourceMemory: resource.MustParse("100Gi"),
				api.ResourcePods:   resource.MustParse("5"),
			},
			Used: api.ResourceList{
				api.ResourceCPU:    resource.MustParse("1"),
				api.ResourceMemory: resource.MustParse("50Gi"),
				api.ResourcePods:   resource.MustParse("3"),
			},
		},
	}
	kubeClient := fake.NewSimpleClientset(resourceQuota)
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{"namespace": cache.MetaNamespaceIndexFunc})
	handler := &quotaAdmission{
		Handler:  admission.NewHandler(admission.Create, admission.Update),
		client:   kubeClient,
		indexer:  indexer,
		registry: install.NewRegistry(kubeClient),
	}
	handler.indexer.Add(resourceQuota)
	newPod := validPod("allowed-pod", 1, getResourceRequirements(getResourceList("100m", "2Gi"), getResourceList("", "")))
	err := handler.Admit(admission.NewAttributesRecord(newPod, api.Kind("Pod"), newPod.Namespace, newPod.Name, api.Resource("pods"), "", admission.Create, nil))
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if len(kubeClient.Actions()) == 0 {
		t.Errorf("Expected a client action")
	}

	expectedActionSet := sets.NewString(
		strings.Join([]string{"update", "resourcequotas", "status"}, "-"),
	)
	actionSet := sets.NewString()
	for _, action := range kubeClient.Actions() {
		actionSet.Insert(strings.Join([]string{action.GetVerb(), action.GetResource(), action.GetSubresource()}, "-"))
	}
	if !actionSet.HasAll(expectedActionSet.List()...) {
		t.Errorf("Expected actions:\n%v\n but got:\n%v\nDifference:\n%v", expectedActionSet, actionSet, expectedActionSet.Difference(actionSet))
	}

	lastActionIndex := len(kubeClient.Actions()) - 1
	usage := kubeClient.Actions()[lastActionIndex].(testclient.UpdateAction).GetObject().(*api.ResourceQuota)
	expectedUsage := api.ResourceQuota{
		Status: api.ResourceQuotaStatus{
			Hard: api.ResourceList{
				api.ResourceCPU:    resource.MustParse("3"),
				api.ResourceMemory: resource.MustParse("100Gi"),
				api.ResourcePods:   resource.MustParse("5"),
			},
			Used: api.ResourceList{
				api.ResourceCPU:    resource.MustParse("1100m"),
				api.ResourceMemory: resource.MustParse("52Gi"),
				api.ResourcePods:   resource.MustParse("4"),
			},
		},
	}
	for k, v := range expectedUsage.Status.Used {
		actual := usage.Status.Used[k]
		actualValue := actual.String()
		expectedValue := v.String()
		if expectedValue != actualValue {
			t.Errorf("Usage Used: Key: %v, Expected: %v, Actual: %v", k, expectedValue, actualValue)
		}
	}
}

// TestAdmitExceedQuotaLimit verifies that if a pod exceeded allowed usage that its rejected during admission.
func TestAdmitExceedQuotaLimit(t *testing.T) {
	resourceQuota := &api.ResourceQuota{
		ObjectMeta: api.ObjectMeta{Name: "quota", Namespace: "test", ResourceVersion: "124"},
		Status: api.ResourceQuotaStatus{
			Hard: api.ResourceList{
				api.ResourceCPU:    resource.MustParse("3"),
				api.ResourceMemory: resource.MustParse("100Gi"),
				api.ResourcePods:   resource.MustParse("5"),
			},
			Used: api.ResourceList{
				api.ResourceCPU:    resource.MustParse("1"),
				api.ResourceMemory: resource.MustParse("50Gi"),
				api.ResourcePods:   resource.MustParse("3"),
			},
		},
	}
	kubeClient := fake.NewSimpleClientset(resourceQuota)
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{"namespace": cache.MetaNamespaceIndexFunc})
	handler := &quotaAdmission{
		Handler:  admission.NewHandler(admission.Create, admission.Update),
		client:   kubeClient,
		indexer:  indexer,
		registry: install.NewRegistry(kubeClient),
	}
	handler.indexer.Add(resourceQuota)
	newPod := validPod("not-allowed-pod", 1, getResourceRequirements(getResourceList("3", "2Gi"), getResourceList("", "")))
	err := handler.Admit(admission.NewAttributesRecord(newPod, api.Kind("Pod"), newPod.Namespace, newPod.Name, api.Resource("pods"), "", admission.Create, nil))
	if err == nil {
		t.Errorf("Expected an error exceeding quota")
	}
}

// TestAdmitEnforceQuotaConstraints verifies that if a quota tracks a particular resource that that resource is
// specified on the pod.  In this case, we create a quota that tracks cpu request, memory request, and memory limit.
// We ensure that a pod that does not specify a memory limit that it fails in admission.
func TestAdmitEnforceQuotaConstraints(t *testing.T) {
	resourceQuota := &api.ResourceQuota{
		ObjectMeta: api.ObjectMeta{Name: "quota", Namespace: "test", ResourceVersion: "124"},
		Status: api.ResourceQuotaStatus{
			Hard: api.ResourceList{
				api.ResourceCPU:          resource.MustParse("3"),
				api.ResourceMemory:       resource.MustParse("100Gi"),
				api.ResourceLimitsMemory: resource.MustParse("200Gi"),
				api.ResourcePods:         resource.MustParse("5"),
			},
			Used: api.ResourceList{
				api.ResourceCPU:          resource.MustParse("1"),
				api.ResourceMemory:       resource.MustParse("50Gi"),
				api.ResourceLimitsMemory: resource.MustParse("100Gi"),
				api.ResourcePods:         resource.MustParse("3"),
			},
		},
	}
	kubeClient := fake.NewSimpleClientset(resourceQuota)
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{"namespace": cache.MetaNamespaceIndexFunc})
	handler := &quotaAdmission{
		Handler:  admission.NewHandler(admission.Create, admission.Update),
		client:   kubeClient,
		indexer:  indexer,
		registry: install.NewRegistry(kubeClient),
	}
	handler.indexer.Add(resourceQuota)
	newPod := validPod("not-allowed-pod", 1, getResourceRequirements(getResourceList("100m", "2Gi"), getResourceList("200m", "")))
	err := handler.Admit(admission.NewAttributesRecord(newPod, api.Kind("Pod"), newPod.Namespace, newPod.Name, api.Resource("pods"), "", admission.Create, nil))
	if err == nil {
		t.Errorf("Expected an error because the pod does not specify a memory limit")
	}
}

// TestAdmitPodInNamespaceWithoutQuota ensures that if a namespace has no quota, that a pod can get in
func TestAdmitPodInNamespaceWithoutQuota(t *testing.T) {
	resourceQuota := &api.ResourceQuota{
		ObjectMeta: api.ObjectMeta{Name: "quota", Namespace: "other", ResourceVersion: "124"},
		Status: api.ResourceQuotaStatus{
			Hard: api.ResourceList{
				api.ResourceCPU:          resource.MustParse("3"),
				api.ResourceMemory:       resource.MustParse("100Gi"),
				api.ResourceLimitsMemory: resource.MustParse("200Gi"),
				api.ResourcePods:         resource.MustParse("5"),
			},
			Used: api.ResourceList{
				api.ResourceCPU:          resource.MustParse("1"),
				api.ResourceMemory:       resource.MustParse("50Gi"),
				api.ResourceLimitsMemory: resource.MustParse("100Gi"),
				api.ResourcePods:         resource.MustParse("3"),
			},
		},
	}
	kubeClient := fake.NewSimpleClientset(resourceQuota)
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{"namespace": cache.MetaNamespaceIndexFunc})
	liveLookupCache, err := lru.New(100)
	if err != nil {
		t.Fatal(err)
	}
	handler := &quotaAdmission{
		Handler:         admission.NewHandler(admission.Create, admission.Update),
		client:          kubeClient,
		indexer:         indexer,
		registry:        install.NewRegistry(kubeClient),
		liveLookupCache: liveLookupCache,
	}
	// Add to the index
	handler.indexer.Add(resourceQuota)
	newPod := validPod("not-allowed-pod", 1, getResourceRequirements(getResourceList("100m", "2Gi"), getResourceList("200m", "")))
	// Add to the lru cache so we do not do a live client lookup
	liveLookupCache.Add(newPod.Namespace, liveLookupEntry{expiry: time.Now().Add(time.Duration(30 * time.Second)), items: []*api.ResourceQuota{}})
	err = handler.Admit(admission.NewAttributesRecord(newPod, api.Kind("Pod"), newPod.Namespace, newPod.Name, api.Resource("pods"), "", admission.Create, nil))
	if err != nil {
		t.Errorf("Did not expect an error because the pod is in a different namespace than the quota")
	}
}

// TestAdmitBelowTerminatingQuotaLimit ensures that terminating pods are charged to the right quota.
// It creates a terminating and non-terminating quota, and creates a terminating pod.
// It ensures that the terminating quota is incremented, and the non-terminating quota is not.
func TestAdmitBelowTerminatingQuotaLimit(t *testing.T) {
	resourceQuotaNonTerminating := &api.ResourceQuota{
		ObjectMeta: api.ObjectMeta{Name: "quota-non-terminating", Namespace: "test", ResourceVersion: "124"},
		Spec: api.ResourceQuotaSpec{
			Scopes: []api.ResourceQuotaScope{api.ResourceQuotaScopeNotTerminating},
		},
		Status: api.ResourceQuotaStatus{
			Hard: api.ResourceList{
				api.ResourceCPU:    resource.MustParse("3"),
				api.ResourceMemory: resource.MustParse("100Gi"),
				api.ResourcePods:   resource.MustParse("5"),
			},
			Used: api.ResourceList{
				api.ResourceCPU:    resource.MustParse("1"),
				api.ResourceMemory: resource.MustParse("50Gi"),
				api.ResourcePods:   resource.MustParse("3"),
			},
		},
	}
	resourceQuotaTerminating := &api.ResourceQuota{
		ObjectMeta: api.ObjectMeta{Name: "quota-terminating", Namespace: "test", ResourceVersion: "124"},
		Spec: api.ResourceQuotaSpec{
			Scopes: []api.ResourceQuotaScope{api.ResourceQuotaScopeTerminating},
		},
		Status: api.ResourceQuotaStatus{
			Hard: api.ResourceList{
				api.ResourceCPU:    resource.MustParse("3"),
				api.ResourceMemory: resource.MustParse("100Gi"),
				api.ResourcePods:   resource.MustParse("5"),
			},
			Used: api.ResourceList{
				api.ResourceCPU:    resource.MustParse("1"),
				api.ResourceMemory: resource.MustParse("50Gi"),
				api.ResourcePods:   resource.MustParse("3"),
			},
		},
	}
	kubeClient := fake.NewSimpleClientset(resourceQuotaTerminating, resourceQuotaNonTerminating)
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{"namespace": cache.MetaNamespaceIndexFunc})
	handler := &quotaAdmission{
		Handler:  admission.NewHandler(admission.Create, admission.Update),
		client:   kubeClient,
		indexer:  indexer,
		registry: install.NewRegistry(kubeClient),
	}
	handler.indexer.Add(resourceQuotaNonTerminating)
	handler.indexer.Add(resourceQuotaTerminating)

	// create a pod that has an active deadline
	newPod := validPod("allowed-pod", 1, getResourceRequirements(getResourceList("100m", "2Gi"), getResourceList("", "")))
	activeDeadlineSeconds := int64(30)
	newPod.Spec.ActiveDeadlineSeconds = &activeDeadlineSeconds
	err := handler.Admit(admission.NewAttributesRecord(newPod, api.Kind("Pod"), newPod.Namespace, newPod.Name, api.Resource("pods"), "", admission.Create, nil))
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if len(kubeClient.Actions()) == 0 {
		t.Errorf("Expected a client action")
	}

	expectedActionSet := sets.NewString(
		strings.Join([]string{"update", "resourcequotas", "status"}, "-"),
	)
	actionSet := sets.NewString()
	for _, action := range kubeClient.Actions() {
		actionSet.Insert(strings.Join([]string{action.GetVerb(), action.GetResource(), action.GetSubresource()}, "-"))
	}
	if !actionSet.HasAll(expectedActionSet.List()...) {
		t.Errorf("Expected actions:\n%v\n but got:\n%v\nDifference:\n%v", expectedActionSet, actionSet, expectedActionSet.Difference(actionSet))
	}

	lastActionIndex := len(kubeClient.Actions()) - 1
	usage := kubeClient.Actions()[lastActionIndex].(testclient.UpdateAction).GetObject().(*api.ResourceQuota)

	// ensure only the quota-terminating was updated
	if usage.Name != resourceQuotaTerminating.Name {
		t.Errorf("Incremented the wrong quota, expected %v, actual %v", resourceQuotaTerminating.Name, usage.Name)
	}

	expectedUsage := api.ResourceQuota{
		Status: api.ResourceQuotaStatus{
			Hard: api.ResourceList{
				api.ResourceCPU:    resource.MustParse("3"),
				api.ResourceMemory: resource.MustParse("100Gi"),
				api.ResourcePods:   resource.MustParse("5"),
			},
			Used: api.ResourceList{
				api.ResourceCPU:    resource.MustParse("1100m"),
				api.ResourceMemory: resource.MustParse("52Gi"),
				api.ResourcePods:   resource.MustParse("4"),
			},
		},
	}
	for k, v := range expectedUsage.Status.Used {
		actual := usage.Status.Used[k]
		actualValue := actual.String()
		expectedValue := v.String()
		if expectedValue != actualValue {
			t.Errorf("Usage Used: Key: %v, Expected: %v, Actual: %v", k, expectedValue, actualValue)
		}
	}
}

// TestAdmitBelowBestEffortQuotaLimit creates a best effort and non-best effort quota.
// It verifies that best effort pods are properly scoped to the best effort quota document.
func TestAdmitBelowBestEffortQuotaLimit(t *testing.T) {
	resourceQuotaBestEffort := &api.ResourceQuota{
		ObjectMeta: api.ObjectMeta{Name: "quota-besteffort", Namespace: "test", ResourceVersion: "124"},
		Spec: api.ResourceQuotaSpec{
			Scopes: []api.ResourceQuotaScope{api.ResourceQuotaScopeBestEffort},
		},
		Status: api.ResourceQuotaStatus{
			Hard: api.ResourceList{
				api.ResourcePods: resource.MustParse("5"),
			},
			Used: api.ResourceList{
				api.ResourcePods: resource.MustParse("3"),
			},
		},
	}
	resourceQuotaNotBestEffort := &api.ResourceQuota{
		ObjectMeta: api.ObjectMeta{Name: "quota-not-besteffort", Namespace: "test", ResourceVersion: "124"},
		Spec: api.ResourceQuotaSpec{
			Scopes: []api.ResourceQuotaScope{api.ResourceQuotaScopeNotBestEffort},
		},
		Status: api.ResourceQuotaStatus{
			Hard: api.ResourceList{
				api.ResourcePods: resource.MustParse("5"),
			},
			Used: api.ResourceList{
				api.ResourcePods: resource.MustParse("3"),
			},
		},
	}
	kubeClient := fake.NewSimpleClientset(resourceQuotaBestEffort, resourceQuotaNotBestEffort)
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{"namespace": cache.MetaNamespaceIndexFunc})
	handler := &quotaAdmission{
		Handler:  admission.NewHandler(admission.Create, admission.Update),
		client:   kubeClient,
		indexer:  indexer,
		registry: install.NewRegistry(kubeClient),
	}
	handler.indexer.Add(resourceQuotaBestEffort)
	handler.indexer.Add(resourceQuotaNotBestEffort)

	// create a pod that is best effort because it does not make a request for anything
	newPod := validPod("allowed-pod", 1, getResourceRequirements(getResourceList("", ""), getResourceList("", "")))
	err := handler.Admit(admission.NewAttributesRecord(newPod, api.Kind("Pod"), newPod.Namespace, newPod.Name, api.Resource("pods"), "", admission.Create, nil))
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	expectedActionSet := sets.NewString(
		strings.Join([]string{"update", "resourcequotas", "status"}, "-"),
	)
	actionSet := sets.NewString()
	for _, action := range kubeClient.Actions() {
		actionSet.Insert(strings.Join([]string{action.GetVerb(), action.GetResource(), action.GetSubresource()}, "-"))
	}
	if !actionSet.HasAll(expectedActionSet.List()...) {
		t.Errorf("Expected actions:\n%v\n but got:\n%v\nDifference:\n%v", expectedActionSet, actionSet, expectedActionSet.Difference(actionSet))
	}
	lastActionIndex := len(kubeClient.Actions()) - 1
	usage := kubeClient.Actions()[lastActionIndex].(testclient.UpdateAction).GetObject().(*api.ResourceQuota)

	if usage.Name != resourceQuotaBestEffort.Name {
		t.Errorf("Incremented the wrong quota, expected %v, actual %v", resourceQuotaBestEffort.Name, usage.Name)
	}

	expectedUsage := api.ResourceQuota{
		Status: api.ResourceQuotaStatus{
			Hard: api.ResourceList{
				api.ResourcePods: resource.MustParse("5"),
			},
			Used: api.ResourceList{
				api.ResourcePods: resource.MustParse("4"),
			},
		},
	}
	for k, v := range expectedUsage.Status.Used {
		actual := usage.Status.Used[k]
		actualValue := actual.String()
		expectedValue := v.String()
		if expectedValue != actualValue {
			t.Errorf("Usage Used: Key: %v, Expected: %v, Actual: %v", k, expectedValue, actualValue)
		}
	}
}

// TestAdmitBestEffortQuotaLimitIgnoresBurstable validates that a besteffort quota does not match a resource
// guaranteed pod.
func TestAdmitBestEffortQuotaLimitIgnoresBurstable(t *testing.T) {
	resourceQuota := &api.ResourceQuota{
		ObjectMeta: api.ObjectMeta{Name: "quota-besteffort", Namespace: "test", ResourceVersion: "124"},
		Spec: api.ResourceQuotaSpec{
			Scopes: []api.ResourceQuotaScope{api.ResourceQuotaScopeBestEffort},
		},
		Status: api.ResourceQuotaStatus{
			Hard: api.ResourceList{
				api.ResourcePods: resource.MustParse("5"),
			},
			Used: api.ResourceList{
				api.ResourcePods: resource.MustParse("3"),
			},
		},
	}
	kubeClient := fake.NewSimpleClientset(resourceQuota)
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{"namespace": cache.MetaNamespaceIndexFunc})
	handler := &quotaAdmission{
		Handler:  admission.NewHandler(admission.Create, admission.Update),
		client:   kubeClient,
		indexer:  indexer,
		registry: install.NewRegistry(kubeClient),
	}
	handler.indexer.Add(resourceQuota)
	newPod := validPod("allowed-pod", 1, getResourceRequirements(getResourceList("100m", "1Gi"), getResourceList("", "")))
	err := handler.Admit(admission.NewAttributesRecord(newPod, api.Kind("Pod"), newPod.Namespace, newPod.Name, api.Resource("pods"), "", admission.Create, nil))
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if len(kubeClient.Actions()) != 0 {
		t.Errorf("Expected no client actions because the incoming pod did not match best effort quota")
	}
}

func TestHasUsageStats(t *testing.T) {
	testCases := map[string]struct {
		a        api.ResourceQuota
		expected bool
	}{
		"empty": {
			a:        api.ResourceQuota{Status: api.ResourceQuotaStatus{Hard: api.ResourceList{}}},
			expected: true,
		},
		"hard-only": {
			a: api.ResourceQuota{
				Status: api.ResourceQuotaStatus{
					Hard: api.ResourceList{
						api.ResourceMemory: resource.MustParse("1Gi"),
					},
					Used: api.ResourceList{},
				},
			},
			expected: false,
		},
		"hard-used": {
			a: api.ResourceQuota{
				Status: api.ResourceQuotaStatus{
					Hard: api.ResourceList{
						api.ResourceMemory: resource.MustParse("1Gi"),
					},
					Used: api.ResourceList{
						api.ResourceMemory: resource.MustParse("500Mi"),
					},
				},
			},
			expected: true,
		},
	}
	for testName, testCase := range testCases {
		if result := hasUsageStats(&testCase.a); result != testCase.expected {
			t.Errorf("%s expected: %v, actual: %v", testName, testCase.expected, result)
		}
	}
}
