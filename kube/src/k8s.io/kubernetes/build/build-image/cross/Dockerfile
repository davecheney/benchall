# Copyright 2016 The Kubernetes Authors All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This file creates a standard build environment for building cross
# platform go binary for the architecture kubernetes cares about.

FROM  golang:1.4.2

ENV GOARM 6
ENV KUBE_DYNAMIC_CROSSPLATFORMS \
  armel

ENV KUBE_CROSSPLATFORMS \
  linux/386 \
  linux/arm \
  darwin/amd64 darwin/386 \
  windows/amd64 windows/386

RUN cd /usr/src/go/src \
  && for platform in ${KUBE_CROSSPLATFORMS}; do GOOS=${platform%/*} GOARCH=${platform##*/} ./make.bash --no-clean; done

# Install g++, then download and install protoc for generating protobuf output
RUN apt-get update \
  && apt-get install -y g++ rsync \
  && apt-get clean && rm -rf /var/lib/apt/lists/*

RUN mkdir -p /usr/local/src/protobuf && cd /usr/local/src/protobuf &&\
    wget -q https://github.com/google/protobuf/releases/download/v3.0.0-beta-2/protobuf-cpp-3.0.0-beta-2.tar.gz &&\
    tar xzvf protobuf-cpp-3.0.0-beta-2.tar.gz &&\
    cd protobuf-3.0.0-beta-2 &&\
    ./configure &&\
    make install &&\
    ldconfig &&\
    cd .. &&\
    rm -rf protobuf-3.0.0-beta-2 &&\
    protoc --version

# Use dynamic cgo linking for architectures other than amd64 for the server platforms
# More info here: https://wiki.debian.org/CrossToolchains
RUN echo "deb http://emdebian.org/tools/debian/ jessie main" > /etc/apt/sources.list.d/cgocrosscompiling.list \
  && curl -s http://emdebian.org/tools/debian/emdebian-toolchain-archive.key | apt-key add - \
  && for platform in ${KUBE_DYNAMIC_CROSSPLATFORMS}; do dpkg --add-architecture ${platform}; done \
  && apt-get update \
  && apt-get install -y build-essential \
  && for platform in ${KUBE_DYNAMIC_CROSSPLATFORMS}; do apt-get install -y crossbuild-essential-${platform}; done \
  && apt-get clean && rm -rf /var/lib/apt/lists/*

# work around 64MB tmpfs size in Docker 1.6
ENV TMPDIR /tmp.k8s

# Get the code coverage tool and godep
RUN mkdir $TMPDIR && \
    go get golang.org/x/tools/cmd/cover github.com/tools/godep

# Download and symlink etcd.  We need this for our integration tests.
RUN export ETCD_VERSION=v2.2.1; \
    mkdir -p /usr/local/src/etcd &&\
    cd /usr/local/src/etcd &&\
    curl -fsSLO https://github.com/coreos/etcd/releases/download/$ETCD_VERSION/etcd-$ETCD_VERSION-linux-amd64.tar.gz &&\
    tar xzf etcd-$ETCD_VERSION-linux-amd64.tar.gz &&\
    ln -s ../src/etcd/etcd-$ETCD_VERSION-linux-amd64/etcd /usr/local/bin/
