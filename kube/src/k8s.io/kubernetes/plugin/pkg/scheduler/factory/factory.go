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

// Package factory can set up a scheduler. This code is here instead of
// plugin/cmd/scheduler for both testability and reuse.
package factory

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/errors"
	"k8s.io/kubernetes/pkg/client/cache"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/controller/framework"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/types"
	"k8s.io/kubernetes/pkg/util/runtime"
	"k8s.io/kubernetes/pkg/util/sets"
	"k8s.io/kubernetes/plugin/pkg/scheduler"
	"k8s.io/kubernetes/plugin/pkg/scheduler/algorithm"
	"k8s.io/kubernetes/plugin/pkg/scheduler/algorithm/predicates"
	schedulerapi "k8s.io/kubernetes/plugin/pkg/scheduler/api"
	"k8s.io/kubernetes/plugin/pkg/scheduler/api/validation"

	"github.com/golang/glog"
	"k8s.io/kubernetes/pkg/apis/extensions"
)

const (
	SchedulerAnnotationKey = "scheduler.alpha.kubernetes.io/name"
)

// ConfigFactory knows how to fill out a scheduler config with its support functions.
type ConfigFactory struct {
	Client *client.Client
	// queue for pods that need scheduling
	PodQueue *cache.FIFO
	// a means to list all known scheduled pods.
	ScheduledPodLister *cache.StoreToPodLister
	// a means to list all known scheduled pods and pods assumed to have been scheduled.
	PodLister algorithm.PodLister
	// a means to list all nodes
	NodeLister *cache.StoreToNodeLister
	// a means to list all PersistentVolumes
	PVLister *cache.StoreToPVFetcher
	// a means to list all PersistentVolumeClaims
	PVCLister *cache.StoreToPVCFetcher
	// a means to list all services
	ServiceLister *cache.StoreToServiceLister
	// a means to list all controllers
	ControllerLister *cache.StoreToReplicationControllerLister
	// a means to list all replicasets
	ReplicaSetLister *cache.StoreToReplicaSetLister

	// Close this to stop all reflectors
	StopEverything chan struct{}

	scheduledPodPopulator *framework.Controller
	modeler               scheduler.SystemModeler

	// SchedulerName of a scheduler is used to select which pods will be
	// processed by this scheduler, based on pods's annotation key:
	// 'scheduler.alpha.kubernetes.io/name'
	SchedulerName string
}

// Initializes the factory.
func NewConfigFactory(client *client.Client, schedulerName string) *ConfigFactory {
	c := &ConfigFactory{
		Client:             client,
		PodQueue:           cache.NewFIFO(cache.MetaNamespaceKeyFunc),
		ScheduledPodLister: &cache.StoreToPodLister{},
		// Only nodes in the "Ready" condition with status == "True" are schedulable
		NodeLister:       &cache.StoreToNodeLister{Store: cache.NewStore(cache.MetaNamespaceKeyFunc)},
		PVLister:         &cache.StoreToPVFetcher{Store: cache.NewStore(cache.MetaNamespaceKeyFunc)},
		PVCLister:        &cache.StoreToPVCFetcher{Store: cache.NewStore(cache.MetaNamespaceKeyFunc)},
		ServiceLister:    &cache.StoreToServiceLister{Store: cache.NewStore(cache.MetaNamespaceKeyFunc)},
		ControllerLister: &cache.StoreToReplicationControllerLister{Store: cache.NewStore(cache.MetaNamespaceKeyFunc)},
		ReplicaSetLister: &cache.StoreToReplicaSetLister{Store: cache.NewStore(cache.MetaNamespaceKeyFunc)},
		StopEverything:   make(chan struct{}),
		SchedulerName:    schedulerName,
	}
	modeler := scheduler.NewSimpleModeler(&cache.StoreToPodLister{Store: c.PodQueue}, c.ScheduledPodLister)
	c.modeler = modeler
	c.PodLister = modeler.PodLister()

	// On add/delete to the scheduled pods, remove from the assumed pods.
	// We construct this here instead of in CreateFromKeys because
	// ScheduledPodLister is something we provide to plug in functions that
	// they may need to call.
	c.ScheduledPodLister.Store, c.scheduledPodPopulator = framework.NewInformer(
		c.createAssignedNonTerminatedPodLW(),
		&api.Pod{},
		0,
		framework.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				if pod, ok := obj.(*api.Pod); ok {
					c.modeler.LockedAction(func() {
						c.modeler.ForgetPod(pod)
					})
				}
			},
			DeleteFunc: func(obj interface{}) {
				c.modeler.LockedAction(func() {
					switch t := obj.(type) {
					case *api.Pod:
						c.modeler.ForgetPod(t)
					case cache.DeletedFinalStateUnknown:
						c.modeler.ForgetPodByKey(t.Key)
					}
				})
			},
		},
	)

	return c
}

// Create creates a scheduler with the default algorithm provider.
func (f *ConfigFactory) Create() (*scheduler.Config, error) {
	return f.CreateFromProvider(DefaultProvider)
}

// Creates a scheduler from the name of a registered algorithm provider.
func (f *ConfigFactory) CreateFromProvider(providerName string) (*scheduler.Config, error) {
	glog.V(2).Infof("Creating scheduler from algorithm provider '%v'", providerName)
	provider, err := GetAlgorithmProvider(providerName)
	if err != nil {
		return nil, err
	}

	return f.CreateFromKeys(provider.FitPredicateKeys, provider.PriorityFunctionKeys, []algorithm.SchedulerExtender{})
}

// Creates a scheduler from the configuration file
func (f *ConfigFactory) CreateFromConfig(policy schedulerapi.Policy) (*scheduler.Config, error) {
	glog.V(2).Infof("Creating scheduler from configuration: %v", policy)

	// validate the policy configuration
	if err := validation.ValidatePolicy(policy); err != nil {
		return nil, err
	}

	predicateKeys := sets.NewString()
	for _, predicate := range policy.Predicates {
		glog.V(2).Infof("Registering predicate: %s", predicate.Name)
		predicateKeys.Insert(RegisterCustomFitPredicate(predicate))
	}

	priorityKeys := sets.NewString()
	for _, priority := range policy.Priorities {
		glog.V(2).Infof("Registering priority: %s", priority.Name)
		priorityKeys.Insert(RegisterCustomPriorityFunction(priority))
	}

	extenders := make([]algorithm.SchedulerExtender, 0)
	if len(policy.ExtenderConfigs) != 0 {
		for ii := range policy.ExtenderConfigs {
			glog.V(2).Infof("Creating extender with config %+v", policy.ExtenderConfigs[ii])
			if extender, err := scheduler.NewHTTPExtender(&policy.ExtenderConfigs[ii], policy.APIVersion); err != nil {
				return nil, err
			} else {
				extenders = append(extenders, extender)
			}
		}
	}
	return f.CreateFromKeys(predicateKeys, priorityKeys, extenders)
}

// Creates a scheduler from a set of registered fit predicate keys and priority keys.
func (f *ConfigFactory) CreateFromKeys(predicateKeys, priorityKeys sets.String, extenders []algorithm.SchedulerExtender) (*scheduler.Config, error) {
	glog.V(2).Infof("creating scheduler with fit predicates '%v' and priority functions '%v", predicateKeys, priorityKeys)
	pluginArgs := PluginFactoryArgs{
		PodLister:        f.PodLister,
		ServiceLister:    f.ServiceLister,
		ControllerLister: f.ControllerLister,
		ReplicaSetLister: f.ReplicaSetLister,
		// All fit predicates only need to consider schedulable nodes.
		NodeLister: f.NodeLister.NodeCondition(getNodeConditionPredicate()),
		NodeInfo:   &predicates.CachedNodeInfo{f.NodeLister},
		PVInfo:     f.PVLister,
		PVCInfo:    f.PVCLister,
	}
	predicateFuncs, err := getFitPredicateFunctions(predicateKeys, pluginArgs)
	if err != nil {
		return nil, err
	}

	priorityConfigs, err := getPriorityFunctionConfigs(priorityKeys, pluginArgs)
	if err != nil {
		return nil, err
	}

	// Watch and queue pods that need scheduling.
	cache.NewReflector(f.createUnassignedNonTerminatedPodLW(), &api.Pod{}, f.PodQueue, 0).RunUntil(f.StopEverything)

	// Begin populating scheduled pods.
	go f.scheduledPodPopulator.Run(f.StopEverything)

	// Watch nodes.
	// Nodes may be listed frequently, so provide a local up-to-date cache.
	cache.NewReflector(f.createNodeLW(), &api.Node{}, f.NodeLister.Store, 0).RunUntil(f.StopEverything)

	// Watch PVs & PVCs
	// They may be listed frequently for scheduling constraints, so provide a local up-to-date cache.
	cache.NewReflector(f.createPersistentVolumeLW(), &api.PersistentVolume{}, f.PVLister.Store, 0).RunUntil(f.StopEverything)
	cache.NewReflector(f.createPersistentVolumeClaimLW(), &api.PersistentVolumeClaim{}, f.PVCLister.Store, 0).RunUntil(f.StopEverything)

	// Watch and cache all service objects. Scheduler needs to find all pods
	// created by the same services or ReplicationControllers/ReplicaSets, so that it can spread them correctly.
	// Cache this locally.
	cache.NewReflector(f.createServiceLW(), &api.Service{}, f.ServiceLister.Store, 0).RunUntil(f.StopEverything)

	// Watch and cache all ReplicationController objects. Scheduler needs to find all pods
	// created by the same services or ReplicationControllers/ReplicaSets, so that it can spread them correctly.
	// Cache this locally.
	cache.NewReflector(f.createControllerLW(), &api.ReplicationController{}, f.ControllerLister.Store, 0).RunUntil(f.StopEverything)

	// Watch and cache all ReplicaSet objects. Scheduler needs to find all pods
	// created by the same services or ReplicationControllers/ReplicaSets, so that it can spread them correctly.
	// Cache this locally.
	cache.NewReflector(f.createReplicaSetLW(), &extensions.ReplicaSet{}, f.ReplicaSetLister.Store, 0).RunUntil(f.StopEverything)

	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	algo := scheduler.NewGenericScheduler(predicateFuncs, priorityConfigs, extenders, f.PodLister, r)

	podBackoff := podBackoff{
		perPodBackoff: map[types.NamespacedName]*backoffEntry{},
		clock:         realClock{},

		defaultDuration: 1 * time.Second,
		maxDuration:     60 * time.Second,
	}

	return &scheduler.Config{
		Modeler: f.modeler,
		// The scheduler only needs to consider schedulable nodes.
		NodeLister: f.NodeLister.NodeCondition(getNodeConditionPredicate()),
		Algorithm:  algo,
		Binder:     &binder{f.Client},
		NextPod: func() *api.Pod {
			return f.getNextPod()
		},
		Error:          f.makeDefaultErrorFunc(&podBackoff, f.PodQueue),
		StopEverything: f.StopEverything,
	}, nil
}

func (f *ConfigFactory) getNextPod() *api.Pod {
	for {
		pod := f.PodQueue.Pop().(*api.Pod)
		if f.responsibleForPod(pod) {
			glog.V(4).Infof("About to try and schedule pod %v", pod.Name)
			return pod
		}
	}
}

func (f *ConfigFactory) responsibleForPod(pod *api.Pod) bool {
	if f.SchedulerName == api.DefaultSchedulerName {
		return pod.Annotations[SchedulerAnnotationKey] == f.SchedulerName || pod.Annotations[SchedulerAnnotationKey] == ""
	} else {
		return pod.Annotations[SchedulerAnnotationKey] == f.SchedulerName
	}
}

func getNodeConditionPredicate() cache.NodeConditionPredicate {
	return func(node api.Node) bool {
		for _, cond := range node.Status.Conditions {
			// We consider the node for scheduling only when its NodeReady condition status
			// is ConditionTrue and its NodeOutOfDisk condition status is ConditionFalse.
			if cond.Type == api.NodeReady && cond.Status != api.ConditionTrue {
				glog.V(4).Infof("Ignoring node %v with %v condition status %v", node.Name, cond.Type, cond.Status)
				return false
			} else if cond.Type == api.NodeOutOfDisk && cond.Status != api.ConditionFalse {
				glog.V(4).Infof("Ignoring node %v with %v condition status %v", node.Name, cond.Type, cond.Status)
				return false
			}
		}
		return true
	}
}

// Returns a cache.ListWatch that finds all pods that need to be
// scheduled.
func (factory *ConfigFactory) createUnassignedNonTerminatedPodLW() *cache.ListWatch {
	selector := fields.ParseSelectorOrDie("spec.nodeName==" + "" + ",status.phase!=" + string(api.PodSucceeded) + ",status.phase!=" + string(api.PodFailed))
	return cache.NewListWatchFromClient(factory.Client, "pods", api.NamespaceAll, selector)
}

// Returns a cache.ListWatch that finds all pods that are
// already scheduled.
// TODO: return a ListerWatcher interface instead?
func (factory *ConfigFactory) createAssignedNonTerminatedPodLW() *cache.ListWatch {
	selector := fields.ParseSelectorOrDie("spec.nodeName!=" + "" + ",status.phase!=" + string(api.PodSucceeded) + ",status.phase!=" + string(api.PodFailed))
	return cache.NewListWatchFromClient(factory.Client, "pods", api.NamespaceAll, selector)
}

// createNodeLW returns a cache.ListWatch that gets all changes to nodes.
func (factory *ConfigFactory) createNodeLW() *cache.ListWatch {
	// TODO: Filter out nodes that doesn't have NodeReady condition.
	fields := fields.Set{api.NodeUnschedulableField: "false"}.AsSelector()
	return cache.NewListWatchFromClient(factory.Client, "nodes", api.NamespaceAll, fields)
}

// createPersistentVolumeLW returns a cache.ListWatch that gets all changes to persistentVolumes.
func (factory *ConfigFactory) createPersistentVolumeLW() *cache.ListWatch {
	return cache.NewListWatchFromClient(factory.Client, "persistentVolumes", api.NamespaceAll, fields.ParseSelectorOrDie(""))
}

// createPersistentVolumeClaimLW returns a cache.ListWatch that gets all changes to persistentVolumeClaims.
func (factory *ConfigFactory) createPersistentVolumeClaimLW() *cache.ListWatch {
	return cache.NewListWatchFromClient(factory.Client, "persistentVolumeClaims", api.NamespaceAll, fields.ParseSelectorOrDie(""))
}

// Returns a cache.ListWatch that gets all changes to services.
func (factory *ConfigFactory) createServiceLW() *cache.ListWatch {
	return cache.NewListWatchFromClient(factory.Client, "services", api.NamespaceAll, fields.ParseSelectorOrDie(""))
}

// Returns a cache.ListWatch that gets all changes to controllers.
func (factory *ConfigFactory) createControllerLW() *cache.ListWatch {
	return cache.NewListWatchFromClient(factory.Client, "replicationControllers", api.NamespaceAll, fields.ParseSelectorOrDie(""))
}

// Returns a cache.ListWatch that gets all changes to replicasets.
func (factory *ConfigFactory) createReplicaSetLW() *cache.ListWatch {
	return cache.NewListWatchFromClient(factory.Client.ExtensionsClient, "replicasets", api.NamespaceAll, fields.ParseSelectorOrDie(""))
}

func (factory *ConfigFactory) makeDefaultErrorFunc(backoff *podBackoff, podQueue *cache.FIFO) func(pod *api.Pod, err error) {
	return func(pod *api.Pod, err error) {
		if err == scheduler.ErrNoNodesAvailable {
			glog.V(4).Infof("Unable to schedule %v %v: no nodes are registered to the cluster; waiting", pod.Namespace, pod.Name)
		} else {
			glog.Errorf("Error scheduling %v %v: %v; retrying", pod.Namespace, pod.Name, err)
		}
		backoff.gc()
		// Retry asynchronously.
		// Note that this is extremely rudimentary and we need a more real error handling path.
		go func() {
			defer runtime.HandleCrash()
			podID := types.NamespacedName{
				Namespace: pod.Namespace,
				Name:      pod.Name,
			}

			entry := backoff.getEntry(podID)
			if !entry.TryWait(backoff.maxDuration) {
				glog.Warningf("Request for pod %v already in flight, abandoning", podID)
				return
			}
			// Get the pod again; it may have changed/been scheduled already.
			pod = &api.Pod{}
			err := factory.Client.Get().Namespace(podID.Namespace).Resource("pods").Name(podID.Name).Do().Into(pod)
			if err != nil {
				if !errors.IsNotFound(err) {
					glog.Errorf("Error getting pod %v for retry: %v; abandoning", podID, err)
				}
				return
			}
			if pod.Spec.NodeName == "" {
				podQueue.AddIfNotPresent(pod)
			}
		}()
	}
}

// nodeEnumerator allows a cache.Poller to enumerate items in an api.NodeList
type nodeEnumerator struct {
	*api.NodeList
}

// Len returns the number of items in the node list.
func (ne *nodeEnumerator) Len() int {
	if ne.NodeList == nil {
		return 0
	}
	return len(ne.Items)
}

// Get returns the item (and ID) with the particular index.
func (ne *nodeEnumerator) Get(index int) interface{} {
	return &ne.Items[index]
}

type binder struct {
	*client.Client
}

// Bind just does a POST binding RPC.
func (b *binder) Bind(binding *api.Binding) error {
	glog.V(2).Infof("Attempting to bind %v to %v", binding.Name, binding.Target.Name)
	ctx := api.WithNamespace(api.NewContext(), binding.Namespace)
	return b.Post().Namespace(api.NamespaceValue(ctx)).Resource("bindings").Body(binding).Do().Error()
	// TODO: use Pods interface for binding once clusters are upgraded
	// return b.Pods(binding.Namespace).Bind(binding)
}

type clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now()
}

// backoffEntry is single threaded.  in particular, it only allows a single action to be waiting on backoff at a time.
// It is expected that all users will only use the public TryWait(...) method
// It is also not safe to copy this object.
type backoffEntry struct {
	backoff     time.Duration
	lastUpdate  time.Time
	reqInFlight int32
}

// tryLock attempts to acquire a lock via atomic compare and swap.
// returns true if the lock was acquired, false otherwise
func (b *backoffEntry) tryLock() bool {
	return atomic.CompareAndSwapInt32(&b.reqInFlight, 0, 1)
}

// unlock returns the lock.  panics if the lock isn't held
func (b *backoffEntry) unlock() {
	if !atomic.CompareAndSwapInt32(&b.reqInFlight, 1, 0) {
		panic(fmt.Sprintf("unexpected state on unlocking: %+v", b))
	}
}

// TryWait tries to acquire the backoff lock, maxDuration is the maximum allowed period to wait for.
func (b *backoffEntry) TryWait(maxDuration time.Duration) bool {
	if !b.tryLock() {
		return false
	}
	defer b.unlock()
	b.wait(maxDuration)
	return true
}

func (entry *backoffEntry) getBackoff(maxDuration time.Duration) time.Duration {
	duration := entry.backoff
	newDuration := time.Duration(duration) * 2
	if newDuration > maxDuration {
		newDuration = maxDuration
	}
	entry.backoff = newDuration
	glog.V(4).Infof("Backing off %s for pod %+v", duration.String(), entry)
	return duration
}

func (entry *backoffEntry) wait(maxDuration time.Duration) {
	time.Sleep(entry.getBackoff(maxDuration))
}

type podBackoff struct {
	perPodBackoff   map[types.NamespacedName]*backoffEntry
	lock            sync.Mutex
	clock           clock
	defaultDuration time.Duration
	maxDuration     time.Duration
}

func (p *podBackoff) getEntry(podID types.NamespacedName) *backoffEntry {
	p.lock.Lock()
	defer p.lock.Unlock()
	entry, ok := p.perPodBackoff[podID]
	if !ok {
		entry = &backoffEntry{backoff: p.defaultDuration}
		p.perPodBackoff[podID] = entry
	}
	entry.lastUpdate = p.clock.Now()
	return entry
}

func (p *podBackoff) gc() {
	p.lock.Lock()
	defer p.lock.Unlock()
	now := p.clock.Now()
	for podID, entry := range p.perPodBackoff {
		if now.Sub(entry.lastUpdate) > p.maxDuration {
			delete(p.perPodBackoff, podID)
		}
	}
}
