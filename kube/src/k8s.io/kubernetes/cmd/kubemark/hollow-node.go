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

package main

import (
	"fmt"
	"runtime"

	docker "github.com/fsouza/go-dockerclient"

	"k8s.io/kubernetes/pkg/api"
	clientset "k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset"
	"k8s.io/kubernetes/pkg/client/record"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/client/unversioned/clientcmd"
	cadvisortest "k8s.io/kubernetes/pkg/kubelet/cadvisor/testing"
	"k8s.io/kubernetes/pkg/kubelet/cm"
	"k8s.io/kubernetes/pkg/kubelet/dockertools"
	"k8s.io/kubernetes/pkg/kubemark"
	proxyconfig "k8s.io/kubernetes/pkg/proxy/config"
	"k8s.io/kubernetes/pkg/util"
	fakeiptables "k8s.io/kubernetes/pkg/util/iptables/testing"
	"k8s.io/kubernetes/pkg/util/sets"

	"github.com/golang/glog"
	"github.com/spf13/pflag"
)

type HollowNodeConfig struct {
	KubeconfigPath      string
	KubeletPort         int
	KubeletReadOnlyPort int
	Morph               string
	NodeName            string
	ServerPort          int
}

const (
	maxPods = 110
)

var knownMorphs = sets.NewString("kubelet", "proxy")

func (c *HollowNodeConfig) addFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.KubeconfigPath, "kubeconfig", "/kubeconfig/kubeconfig", "Path to kubeconfig file.")
	fs.IntVar(&c.KubeletPort, "kubelet-port", 10250, "Port on which HollowKubelet should be listening.")
	fs.IntVar(&c.KubeletReadOnlyPort, "kubelet-read-only-port", 10255, "Read-only port on which Kubelet is listening.")
	fs.StringVar(&c.NodeName, "name", "fake-node", "Name of this Hollow Node.")
	fs.IntVar(&c.ServerPort, "api-server-port", 443, "Port on which API server is listening.")
	fs.StringVar(&c.Morph, "morph", "", fmt.Sprintf("Specifies into which Hollow component this binary should morph. Allowed values: %v", knownMorphs.List()))
}

func createClientFromFile(path string) (*client.Client, error) {
	c, err := clientcmd.LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("error while loading kubeconfig from file %v: %v", path, err)
	}
	config, err := clientcmd.NewDefaultClientConfig(*c, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("error while creating kubeconfig: %v", err)
	}
	client, err := client.New(config)
	if err != nil {
		return nil, fmt.Errorf("error while creating client: %v", err)
	}
	return client, nil
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	config := HollowNodeConfig{}
	config.addFlags(pflag.CommandLine)
	util.InitFlags()

	if !knownMorphs.Has(config.Morph) {
		glog.Fatalf("Unknown morph: %v. Allowed values: %v", config.Morph, knownMorphs.List())
	}

	// create a client to communicate with API server.
	cl, err := createClientFromFile(config.KubeconfigPath)
	clientset := clientset.FromUnversionedClient(cl)
	if err != nil {
		glog.Fatal("Failed to create a Client. Exiting.")
	}

	if config.Morph == "kubelet" {
		cadvisorInterface := new(cadvisortest.Fake)
		containerManager := cm.NewStubContainerManager()

		fakeDockerClient := dockertools.NewFakeDockerClient()
		fakeDockerClient.VersionInfo = docker.Env{"Version=1.1.3", "ApiVersion=1.18"}
		fakeDockerClient.EnableSleep = true

		hollowKubelet := kubemark.NewHollowKubelet(
			config.NodeName,
			clientset,
			cadvisorInterface,
			fakeDockerClient,
			config.KubeletPort,
			config.KubeletReadOnlyPort,
			containerManager,
			maxPods,
		)
		hollowKubelet.Run()
	}

	if config.Morph == "proxy" {
		eventBroadcaster := record.NewBroadcaster()
		recorder := eventBroadcaster.NewRecorder(api.EventSource{Component: "kube-proxy", Host: config.NodeName})

		iptInterface := fakeiptables.NewFake()

		serviceConfig := proxyconfig.NewServiceConfig()
		serviceConfig.RegisterHandler(&kubemark.FakeProxyHandler{})

		endpointsConfig := proxyconfig.NewEndpointsConfig()
		endpointsConfig.RegisterHandler(&kubemark.FakeProxyHandler{})

		hollowProxy := kubemark.NewHollowProxyOrDie(config.NodeName, cl, endpointsConfig, serviceConfig, iptInterface, eventBroadcaster, recorder)
		hollowProxy.Run()
	}
}
