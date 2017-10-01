package juju_test

import (
	"os/exec"
	"runtime"
	"testing"
)

func BenchmarkJuju(b *testing.B) {
	for n := 0; n < b.N; n++ {
		cmd := exec.Command("bash", "-c", "env GOPATH=$(pwd)/juju "+runtime.GOROOT()+"/bin/go build -o /tmp/jujud github.com/juju/juju/cmd/jujud")
		if err := cmd.Run(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkKube(b *testing.B) {
	for n := 0; n < b.N; n++ {
		cmd := exec.Command("bash", "-c", "env GOPATH=$(pwd)/kube:$(pwd)/kube/src/k8s.io/kubernetes/Godeps/_workspace "+runtime.GOROOT()+"/bin/go build -o /tmp/kube-controller-manager k8s.io/kubernetes/cmd/kube-controller-manager")
		if err := cmd.Run(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGogs(b *testing.B) {
	for n := 0; n < b.N; n++ {
		cmd := exec.Command("bash", "-c", "env GOPATH=$(pwd)/gogs "+runtime.GOROOT()+"/bin/go build -o /tmp/gogs github.com/gogits/gogs")
		if err := cmd.Run(); err != nil {
			b.Fatal(err)
		}
	}
}
