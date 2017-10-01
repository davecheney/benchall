#!/bin/bash

# Copyright 2014 The Kubernetes Authors All rights reserved.
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

# Script to test cluster/update-storage-objects.sh works as expected.

set -o errexit
set -o nounset
set -o pipefail

KUBE_ROOT=$(dirname "${BASH_SOURCE}")/..
source "${KUBE_ROOT}/hack/lib/init.sh"

# The api version in which objects are currently stored in etcd.
KUBE_OLD_API_VERSION=${KUBE_OLD_API_VERSION:-"v1"}
# The api version in which our etcd objects should be converted to.
# The new api version
KUBE_NEW_API_VERSION=${KUBE_NEW_API_VERSION:-"v1"}

KUBE_OLD_STORAGE_VERSIONS=${KUBE_OLD_STORAGE_VERSIONs:-""}
KUBE_NEW_STORAGE_VERSIONS=${KUBE_NEW_STORAGE_VERSIONs:-""}

ETCD_HOST=${ETCD_HOST:-127.0.0.1}
ETCD_PORT=${ETCD_PORT:-4001}
API_PORT=${API_PORT:-8080}
API_HOST=${API_HOST:-127.0.0.1}
KUBE_API_VERSIONS=""
RUNTIME_CONFIG=""

KUBECTL="${KUBE_OUTPUT_HOSTBIN}/kubectl"
UPDATE_ETCD_OBJECTS_SCRIPT="${KUBE_ROOT}/cluster/update-storage-objects.sh"

function startApiServer() {
  local storage_versions=${1:-""}
  kube::log::status "Starting kube-apiserver with KUBE_API_VERSIONS: ${KUBE_API_VERSIONS}"
  kube::log::status "                            and runtime-config: ${RUNTIME_CONFIG}"
  kube::log::status "                 and storage-version overrides: ${storage_versions}"

  KUBE_API_VERSIONS="${KUBE_API_VERSIONS}" \
    "${KUBE_OUTPUT_HOSTBIN}/kube-apiserver" \
    --insecure-bind-address="${API_HOST}" \
    --bind-address="${API_HOST}" \
    --insecure-port="${API_PORT}" \
    --etcd-servers="http://${ETCD_HOST}:${ETCD_PORT}" \
    --runtime-config="${RUNTIME_CONFIG}" \
    --cert-dir="${TMPDIR:-/tmp/}" \
    --service-cluster-ip-range="10.0.0.0/24" \
    --storage-versions="${storage_versions}" 1>&2 &
  APISERVER_PID=$!

  # url, prefix, wait, times
  kube::util::wait_for_url "http://${API_HOST}:${API_PORT}/healthz" "apiserver: " 1 120
}

function killApiServer() {
  kube::log::status "Killing api server"
  if [[ -n ${APISERVER_PID-} ]]; then
    kill ${APISERVER_PID} 1>&2 2>/dev/null
    wait ${APISERVER_PID} || true
    kube::log::status "api server exited"
  fi
  unset APISERVER_PID
}

function cleanup() {
  killApiServer

  kube::etcd::cleanup

  kube::log::status "Clean up complete"
}

trap cleanup EXIT SIGINT

"${KUBE_ROOT}/hack/build-go.sh" cmd/kube-apiserver

kube::etcd::start

### BEGIN TEST DEFINITION CUSTOMIZATION ###

# source_file,resource,namespace,name,old_version,new_version
tests=(
docs/user-guide/job.yaml,jobs,default,pi,extensions/v1beta1,batch/v1
docs/user-guide/horizontal-pod-autoscaling/hpa-php-apache.yaml,horizontalpodautoscalers,default,php-apache,extensions/v1beta1,autoscaling/v1
)

# need to include extensions/v1beta1 in new api version because its internal types are used by jobs
# and hpas
KUBE_OLD_API_VERSION="v1,extensions/v1beta1"
KUBE_NEW_API_VERSION="v1,extensions/v1beta1,batch/v1,autoscaling/v1"
KUBE_OLD_STORAGE_VERSIONS="batch=extensions/v1beta1,autoscaling=extensions/v1beta1"
KUBE_NEW_STORAGE_VERSIONS="batch/v1,autoscaling/v1"

### END TEST DEFINITION CUSTOMIZATION ###


#######################################################
# Step 1: Start a server which supports both the old and new api versions,
# but KUBE_OLD_API_VERSION is the latest (storage) version.
#######################################################
KUBE_API_VERSIONS="${KUBE_OLD_API_VERSION},${KUBE_NEW_API_VERSION}"
RUNTIME_CONFIG="api/all=false,api/${KUBE_OLD_API_VERSION}=true,api/${KUBE_NEW_API_VERSION}=true"
startApiServer ${KUBE_OLD_STORAGE_VERSIONS}


# Create object(s)
for test in ${tests[@]}; do
  IFS=',' read -ra test_data <<<"$test"
  source_file=${test_data[0]}

  kube::log::status "Creating ${source_file}"
  ${KUBECTL} create -f "${source_file}"

  # Verify that the storage version is the old version
  resource=${test_data[1]}
  namespace=${test_data[2]}
  name=${test_data[3]}
  old_storage_version=${test_data[4]}

  kube::log::status "Verifying ${resource}/${namespace}/${name} has storage version ${old_storage_version} in etcd"
  curl -s http://${ETCD_HOST}:${ETCD_PORT}/v2/keys/registry/${resource}/${namespace}/${name} | grep ${old_storage_version}
done

killApiServer


#######################################################
# Step 2: Start a server which supports both the old and new api versions,
# but KUBE_NEW_API_VERSION is the latest (storage) version.
#######################################################

KUBE_API_VERSIONS="${KUBE_NEW_API_VERSION},${KUBE_OLD_API_VERSION}"
RUNTIME_CONFIG="api/all=false,api/${KUBE_OLD_API_VERSION}=true,api/${KUBE_NEW_API_VERSION}=true"
startApiServer ${KUBE_NEW_STORAGE_VERSIONS}

# Update etcd objects, so that will now be stored in the new api version.
kube::log::status "Updating storage versions in etcd"
${UPDATE_ETCD_OBJECTS_SCRIPT}

# Verify that the storage version was changed in etcd
for test in ${tests[@]}; do
  IFS=',' read -ra test_data <<<"$test"
  resource=${test_data[1]}
  namespace=${test_data[2]}
  name=${test_data[3]}
  new_storage_version=${test_data[5]}

  kube::log::status "Verifying ${resource}/${namespace}/${name} has updated storage version ${new_storage_version} in etcd"
  curl -s http://${ETCD_HOST}:${ETCD_PORT}/v2/keys/registry/${resource}/${namespace}/${name} | grep ${new_storage_version}
done

killApiServer


#######################################################
# Step 3 : Start a server which supports only the new api version.
#######################################################

KUBE_API_VERSIONS="${KUBE_NEW_API_VERSION}"
RUNTIME_CONFIG="api/all=false,api/${KUBE_NEW_API_VERSION}=true"

# This seems to reduce flakiness.
sleep 1
startApiServer ${KUBE_NEW_STORAGE_VERSIONS}

for test in ${tests[@]}; do
  IFS=',' read -ra test_data <<<"$test"
  resource=${test_data[1]}
  namespace=${test_data[2]}
  name=${test_data[3]}

  # Verify that the server is able to read the object.
  kube::log::status "Verifying we can retrieve ${resource}/${namespace}/${name} via kubectl"
  ${KUBECTL} get --namespace=${namespace} ${resource}/${name}
done

killApiServer
