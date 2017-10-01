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

# This command checks that the built commands can function together for
# simple scenarios.  It does not require Docker so it can run in travis.

set -o errexit
set -o nounset
set -o pipefail

KUBE_ROOT=$(dirname "${BASH_SOURCE}")/..
source "${KUBE_ROOT}/hack/lib/init.sh"
source "${KUBE_ROOT}/hack/lib/test.sh"

# Stops the running kubectl proxy, if there is one.
function stop-proxy()
{
  [[ -n "${PROXY_PORT-}" ]] && kube::log::status "Stopping proxy on port ${PROXY_PORT}"
  [[ -n "${PROXY_PID-}" ]] && kill "${PROXY_PID}" 1>&2 2>/dev/null
  [[ -n "${PROXY_PORT_FILE-}" ]] && rm -f ${PROXY_PORT_FILE}
  PROXY_PID=
  PROXY_PORT=
  PROXY_PORT_FILE=
}

# Starts "kubect proxy" to test the client proxy. $1: api_prefix
function start-proxy()
{
  stop-proxy

  PROXY_PORT_FILE=$(mktemp proxy-port.out.XXXXX)
  kube::log::status "Starting kubectl proxy on random port; output file in ${PROXY_PORT_FILE}; args: ${1-}"


  if [ $# -eq 0 ]; then
    kubectl proxy --port=0 --www=. 1>${PROXY_PORT_FILE} 2>&1 &
  else
    kubectl proxy --port=0 --www=. --api-prefix="$1" 1>${PROXY_PORT_FILE} 2>&1 &
  fi
  PROXY_PID=$!
  PROXY_PORT=

  local attempts=0
  while [[ -z ${PROXY_PORT} ]]; do
    if (( ${attempts} > 9 )); then
      kill "${PROXY_PID}"
      kube::log::error_exit "Couldn't start proxy. Failed to read port after ${attempts} tries. Got: $(cat ${PROXY_PORT_FILE})"
    fi
    sleep .5
    kube::log::status "Attempt ${attempts} to read ${PROXY_PORT_FILE}..."
    PROXY_PORT=$(sed 's/.*Starting to serve on 127.0.0.1:\([0-9]*\)$/\1/'< ${PROXY_PORT_FILE})
    attempts=$((attempts+1))
  done

  kube::log::status "kubectl proxy running on port ${PROXY_PORT}"

  # We try checking kubectl proxy 30 times with 1s delays to avoid occasional
  # failures.
  if [ $# -eq 0 ]; then
    kube::util::wait_for_url "http://127.0.0.1:${PROXY_PORT}/healthz" "kubectl proxy"
  else
    kube::util::wait_for_url "http://127.0.0.1:${PROXY_PORT}/$1/healthz" "kubectl proxy --api-prefix=$1"
  fi
}

function cleanup()
{
  [[ -n "${APISERVER_PID-}" ]] && kill "${APISERVER_PID}" 1>&2 2>/dev/null
  [[ -n "${CTLRMGR_PID-}" ]] && kill "${CTLRMGR_PID}" 1>&2 2>/dev/null
  [[ -n "${KUBELET_PID-}" ]] && kill "${KUBELET_PID}" 1>&2 2>/dev/null
  stop-proxy

  kube::etcd::cleanup
  rm -rf "${KUBE_TEMP}"

  kube::log::status "Clean up complete"
}

# Executes curl against the proxy. $1 is the path to use, $2 is the desired
# return code. Prints a helpful message on failure.
function check-curl-proxy-code()
{
  local status
  local -r address=$1
  local -r desired=$2
  local -r full_address="${PROXY_HOST}:${PROXY_PORT}${address}"
  status=$(curl -w "%{http_code}" --silent --output /dev/null "${full_address}")
  if [ "${status}" == "${desired}" ]; then
    return 0
  fi
  echo "For address ${full_address}, got ${status} but wanted ${desired}"
  return 1
}

# TODO: Remove this function when we do the retry inside the kubectl commands. See #15333.
function kubectl-with-retry()
{
  ERROR_FILE="${KUBE_TEMP}/kubectl-error"
  for count in $(seq 0 3); do
    kubectl "$@" 2> ${ERROR_FILE} || true
    if grep -q "the object has been modified" "${ERROR_FILE}"; then
      kube::log::status "retry $1, error: $(cat ${ERROR_FILE})"
      rm "${ERROR_FILE}"
      sleep $((2**count))
    else
      rm "${ERROR_FILE}"
      break
    fi
  done
}

kube::util::trap_add cleanup EXIT SIGINT

kube::util::ensure-temp-dir
kube::etcd::start

ETCD_HOST=${ETCD_HOST:-127.0.0.1}
ETCD_PORT=${ETCD_PORT:-4001}
API_PORT=${API_PORT:-8080}
API_HOST=${API_HOST:-127.0.0.1}
KUBELET_PORT=${KUBELET_PORT:-10250}
KUBELET_HEALTHZ_PORT=${KUBELET_HEALTHZ_PORT:-10248}
CTLRMGR_PORT=${CTLRMGR_PORT:-10252}
PROXY_HOST=127.0.0.1 # kubectl only serves on localhost.

# ensure ~/.kube/config isn't loaded by tests
HOME="${KUBE_TEMP}"

# Check kubectl
kube::log::status "Running kubectl with no options"
"${KUBE_OUTPUT_HOSTBIN}/kubectl"

kube::log::status "Starting kubelet in masterless mode"
"${KUBE_OUTPUT_HOSTBIN}/kubelet" \
  --really-crash-for-testing=true \
  --root-dir=/tmp/kubelet.$$ \
  --cert-dir="${TMPDIR:-/tmp/}" \
  --docker-endpoint="fake://" \
  --hostname-override="127.0.0.1" \
  --address="127.0.0.1" \
  --port="$KUBELET_PORT" \
  --healthz-port="${KUBELET_HEALTHZ_PORT}" 1>&2 &
KUBELET_PID=$!
kube::util::wait_for_url "http://127.0.0.1:${KUBELET_HEALTHZ_PORT}/healthz" "kubelet(masterless)"
kill ${KUBELET_PID} 1>&2 2>/dev/null

kube::log::status "Starting kubelet in masterful mode"
"${KUBE_OUTPUT_HOSTBIN}/kubelet" \
  --really-crash-for-testing=true \
  --root-dir=/tmp/kubelet.$$ \
  --cert-dir="${TMPDIR:-/tmp/}" \
  --docker-endpoint="fake://" \
  --hostname-override="127.0.0.1" \
  --address="127.0.0.1" \
  --api-servers="${API_HOST}:${API_PORT}" \
  --port="$KUBELET_PORT" \
  --healthz-port="${KUBELET_HEALTHZ_PORT}" 1>&2 &
KUBELET_PID=$!

kube::util::wait_for_url "http://127.0.0.1:${KUBELET_HEALTHZ_PORT}/healthz" "kubelet"

# Start kube-apiserver
kube::log::status "Starting kube-apiserver"

# Admission Controllers to invoke prior to persisting objects in cluster
ADMISSION_CONTROL="NamespaceLifecycle,LimitRanger,ResourceQuota"

KUBE_API_VERSIONS="v1,autoscaling/v1,batch/v1,extensions/v1beta1" "${KUBE_OUTPUT_HOSTBIN}/kube-apiserver" \
  --address="127.0.0.1" \
  --public-address-override="127.0.0.1" \
  --port="${API_PORT}" \
  --admission-control="${ADMISSION_CONTROL}" \
  --etcd-servers="http://${ETCD_HOST}:${ETCD_PORT}" \
  --public-address-override="127.0.0.1" \
  --kubelet-port=${KUBELET_PORT} \
  --runtime-config=api/v1 \
  --cert-dir="${TMPDIR:-/tmp/}" \
  --service-cluster-ip-range="10.0.0.0/24" 1>&2 &
APISERVER_PID=$!

kube::util::wait_for_url "http://127.0.0.1:${API_PORT}/healthz" "apiserver"

# Start controller manager
kube::log::status "Starting controller-manager"
"${KUBE_OUTPUT_HOSTBIN}/kube-controller-manager" \
  --port="${CTLRMGR_PORT}" \
  --master="127.0.0.1:${API_PORT}" 1>&2 &
CTLRMGR_PID=$!

kube::util::wait_for_url "http://127.0.0.1:${CTLRMGR_PORT}/healthz" "controller-manager"
kube::util::wait_for_url "http://127.0.0.1:${API_PORT}/api/v1/nodes/127.0.0.1" "apiserver(nodes)"

# Expose kubectl directly for readability
PATH="${KUBE_OUTPUT_HOSTBIN}":$PATH

kube::log::status "Checking kubectl version"
kubectl version

# TODO: we need to note down the current default namespace and set back to this
# namespace after the tests are done.
kubectl config view
CONTEXT="test"
kubectl config set-context "${CONTEXT}"
kubectl config use-context "${CONTEXT}"

i=0
create_and_use_new_namespace() {
  i=$(($i+1))
  kubectl create namespace "namespace${i}"
  kubectl config set-context "${CONTEXT}" --namespace="namespace${i}"
}

runTests() {
  version="$1"
  echo "Testing api version: $1"
  if [[ -z "${version}" ]]; then
    kube_flags=(
      -s "http://127.0.0.1:${API_PORT}"
      --match-server-version
    )
    [ "$(kubectl get nodes -o go-template='{{ .apiVersion }}' "${kube_flags[@]}")" == "v1" ]
  else
    kube_flags=(
      -s "http://127.0.0.1:${API_PORT}"
      --match-server-version
      --api-version="${version}"
    )
    [ "$(kubectl get nodes -o go-template='{{ .apiVersion }}' "${kube_flags[@]}")" == "${version}" ]
  fi
  id_field=".metadata.name"
  labels_field=".metadata.labels"
  annotations_field=".metadata.annotations"
  service_selector_field=".spec.selector"
  rc_replicas_field=".spec.replicas"
  rc_status_replicas_field=".status.replicas"
  rc_container_image_field=".spec.template.spec.containers"
  rs_replicas_field=".spec.replicas"
  port_field="(index .spec.ports 0).port"
  port_name="(index .spec.ports 0).name"
  second_port_field="(index .spec.ports 1).port"
  second_port_name="(index .spec.ports 1).name"
  image_field="(index .spec.containers 0).image"
  hpa_min_field=".spec.minReplicas"
  hpa_max_field=".spec.maxReplicas"
  hpa_cpu_field=".spec.cpuUtilization.targetPercentage"
  job_parallelism_field=".spec.parallelism"
  deployment_replicas=".spec.replicas"
  secret_data=".data"
  secret_type=".type"
  deployment_image_field="(index .spec.template.spec.containers 0).image"
  change_cause_annotation='.*kubernetes.io/change-cause.*'

  # Passing no arguments to create is an error
  ! kubectl create

  #######################
  # kubectl local proxy #
  #######################

  # Make sure the UI can be proxied
  start-proxy
  check-curl-proxy-code /ui 301
  check-curl-proxy-code /metrics 200
  check-curl-proxy-code /api/ui 404
  if [[ -n "${version}" ]]; then
    check-curl-proxy-code /api/${version}/namespaces 200
  fi
  check-curl-proxy-code /static/ 200
  stop-proxy

  # Make sure the in-development api is accessible by default
  start-proxy
  check-curl-proxy-code /apis 200
  check-curl-proxy-code /apis/extensions/ 200
  stop-proxy

  # Custom paths let you see everything.
  start-proxy /custom
  check-curl-proxy-code /custom/ui 301
  check-curl-proxy-code /custom/metrics 200
  if [[ -n "${version}" ]]; then
    check-curl-proxy-code /custom/api/${version}/namespaces 200
  fi
  stop-proxy

  #########################
  # RESTMapper evaluation #
  #########################

  kube::log::status "Testing RESTMapper"

  RESTMAPPER_ERROR_FILE="${KUBE_TEMP}/restmapper-error"

  ### Non-existent resource type should give a recognizeable error
  # Pre-condition: None
  # Command
  kubectl get "${kube_flags[@]}" unknownresourcetype 2>${RESTMAPPER_ERROR_FILE} || true
  if grep -q "the server doesn't have a resource type" "${RESTMAPPER_ERROR_FILE}"; then
    kube::log::status "\"kubectl get unknownresourcetype\" returns error as expected: $(cat ${RESTMAPPER_ERROR_FILE})"
  else
    kube::log::status "\"kubectl get unknownresourcetype\" returns unexpected error or non-error: $(cat ${RESTMAPPER_ERROR_FILE})"
    exit 1
  fi
  rm "${RESTMAPPER_ERROR_FILE}"
  # Post-condition: None

  ###########################
  # POD creation / deletion #
  ###########################

  kube::log::status "Testing kubectl(${version}:pods)"

  ### Create POD valid-pod from JSON
  # Pre-condition: no POD exists
  create_and_use_new_namespace
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create "${kube_flags[@]}" -f docs/admin/limitrange/valid-pod.yaml
  # Post-condition: valid-pod POD is created
  kubectl get "${kube_flags[@]}" pods -o json
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'
  kube::test::get_object_assert 'pod valid-pod' "{{$id_field}}" 'valid-pod'
  kube::test::get_object_assert 'pod/valid-pod' "{{$id_field}}" 'valid-pod'
  kube::test::get_object_assert 'pods/valid-pod' "{{$id_field}}" 'valid-pod'
  # Repeat above test using jsonpath template
  kube::test::get_object_jsonpath_assert pods "{.items[*]$id_field}" 'valid-pod'
  kube::test::get_object_jsonpath_assert 'pod valid-pod' "{$id_field}" 'valid-pod'
  kube::test::get_object_jsonpath_assert 'pod/valid-pod' "{$id_field}" 'valid-pod'
  kube::test::get_object_jsonpath_assert 'pods/valid-pod' "{$id_field}" 'valid-pod'
  # Describe command should print detailed information
  kube::test::describe_object_assert pods 'valid-pod' "Name:" "Image:" "Node:" "Labels:" "Status:" "Controllers"
  # Describe command (resource only) should print detailed information
  kube::test::describe_resource_assert pods "Name:" "Image:" "Node:" "Labels:" "Status:" "Controllers"

  ### Validate Export ###
  kube::test::get_object_assert 'pods/valid-pod' "{{.metadata.namespace}} {{.metadata.name}}" '<no value> valid-pod' "--export=true"

  ### Dump current valid-pod POD
  output_pod=$(kubectl get pod valid-pod -o yaml --output-version=v1 "${kube_flags[@]}")

  ### Delete POD valid-pod by id
  # Pre-condition: valid-pod POD exists
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'
  # Command
  kubectl delete pod valid-pod "${kube_flags[@]}" --grace-period=0
  # Post-condition: valid-pod POD doesn't exist
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''

  ### Create POD valid-pod from dumped YAML
  # Pre-condition: no POD exists
  create_and_use_new_namespace
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  echo "${output_pod}" | sed '/namespace:/d' | kubectl create -f - "${kube_flags[@]}"
  # Post-condition: valid-pod POD is created
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'

  ### Delete POD valid-pod from JSON
  # Pre-condition: valid-pod POD exists
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'
  # Command
  kubectl delete -f docs/admin/limitrange/valid-pod.yaml "${kube_flags[@]}" --grace-period=0
  # Post-condition: valid-pod POD doesn't exist
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''

  ### Create POD valid-pod from JSON
  # Pre-condition: no POD exists
  create_and_use_new_namespace
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create -f docs/admin/limitrange/valid-pod.yaml "${kube_flags[@]}"
  # Post-condition: valid-pod POD is created
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'

  ### Delete POD valid-pod with label
  # Pre-condition: valid-pod POD exists
  kube::test::get_object_assert "pods -l'name in (valid-pod)'" '{{range.items}}{{$id_field}}:{{end}}' 'valid-pod:'
  # Command
  kubectl delete pods -l'name in (valid-pod)' "${kube_flags[@]}" --grace-period=0
  # Post-condition: valid-pod POD doesn't exist
  kube::test::get_object_assert "pods -l'name in (valid-pod)'" '{{range.items}}{{$id_field}}:{{end}}' ''

  ### Create POD valid-pod from YAML
  # Pre-condition: no POD exists
  create_and_use_new_namespace
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create -f docs/admin/limitrange/valid-pod.yaml "${kube_flags[@]}"
  # Post-condition: valid-pod POD is created
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'

  ### Delete PODs with no parameter mustn't kill everything
  # Pre-condition: valid-pod POD exists
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'
  # Command
  ! kubectl delete pods "${kube_flags[@]}"
  # Post-condition: valid-pod POD exists
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'

  ### Delete PODs with --all and a label selector is not permitted
  # Pre-condition: valid-pod POD exists
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'
  # Command
  ! kubectl delete --all pods -l'name in (valid-pod)' "${kube_flags[@]}"
  # Post-condition: valid-pod POD exists
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'

  ### Delete all PODs
  # Pre-condition: valid-pod POD exists
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'
  # Command
  kubectl delete --all pods "${kube_flags[@]}" --grace-period=0 # --all remove all the pods
  # Post-condition: no POD exists
  kube::test::get_object_assert "pods -l'name in (valid-pod)'" '{{range.items}}{{$id_field}}:{{end}}' ''

  ### Create two PODs
  # Pre-condition: no POD exists
  create_and_use_new_namespace
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create -f docs/admin/limitrange/valid-pod.yaml "${kube_flags[@]}"
  kubectl create -f examples/redis/redis-proxy.yaml "${kube_flags[@]}"
  # Post-condition: valid-pod and redis-proxy PODs are created
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'redis-proxy:valid-pod:'

  ### Delete multiple PODs at once
  # Pre-condition: valid-pod and redis-proxy PODs exist
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'redis-proxy:valid-pod:'
  # Command
  kubectl delete pods valid-pod redis-proxy "${kube_flags[@]}" --grace-period=0 # delete multiple pods at once
  # Post-condition: no POD exists
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''

  ### Create valid-pod POD
  # Pre-condition: no POD exists
  create_and_use_new_namespace
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create -f docs/admin/limitrange/valid-pod.yaml "${kube_flags[@]}"
  # Post-condition: valid-pod POD is created
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'

  ### Label the valid-pod POD
  # Pre-condition: valid-pod is not labelled
  kube::test::get_object_assert 'pod valid-pod' "{{range$labels_field}}{{.}}:{{end}}" 'valid-pod:'
  # Command
  kubectl label pods valid-pod new-name=new-valid-pod "${kube_flags[@]}"
  # Post-condition: valid-pod is labelled
  kube::test::get_object_assert 'pod valid-pod' "{{range$labels_field}}{{.}}:{{end}}" 'valid-pod:new-valid-pod:'

  ### Delete POD by label
  # Pre-condition: valid-pod POD exists
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'
  # Command
  kubectl delete pods -lnew-name=new-valid-pod --grace-period=0 "${kube_flags[@]}"
  # Post-condition: valid-pod POD doesn't exist
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''

  ### Create valid-pod POD
  # Pre-condition: no POD exists
  create_and_use_new_namespace
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create -f docs/admin/limitrange/valid-pod.yaml "${kube_flags[@]}"
  # Post-condition: valid-pod POD is created
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'

  ## Patch pod can change image
  # Command
  kubectl patch "${kube_flags[@]}" pod valid-pod --record -p='{"spec":{"containers":[{"name": "kubernetes-serve-hostname", "image": "nginx"}]}}'
  # Post-condition: valid-pod POD has image nginx
  kube::test::get_object_assert pods "{{range.items}}{{$image_field}}:{{end}}" 'nginx:'
  # Post-condition: valid-pod has the record annotation
  kube::test::get_object_assert pods "{{range.items}}{{$annotations_field}}:{{end}}" "${change_cause_annotation}"
  # prove that patch can use different types 
  kubectl patch "${kube_flags[@]}" pod valid-pod --type="json" -p='[{"op": "replace", "path": "/spec/containers/0/image", "value":"nginx2"}]'
  # Post-condition: valid-pod POD has image nginx
  kube::test::get_object_assert pods "{{range.items}}{{$image_field}}:{{end}}" 'nginx2:'
  # prove that patch can use different types 
  kubectl patch "${kube_flags[@]}" pod valid-pod --type="json" -p='[{"op": "replace", "path": "/spec/containers/0/image", "value":"nginx"}]'
  # Post-condition: valid-pod POD has image nginx
  kube::test::get_object_assert pods "{{range.items}}{{$image_field}}:{{end}}" 'nginx:'
  # prove that yaml input works too
  YAML_PATCH=$'spec:\n  containers:\n  - name: kubernetes-serve-hostname\n    image: changed-with-yaml\n'
  kubectl patch "${kube_flags[@]}" pod valid-pod -p="${YAML_PATCH}"
  # Post-condition: valid-pod POD has image nginx
  kube::test::get_object_assert pods "{{range.items}}{{$image_field}}:{{end}}" 'changed-with-yaml:'
  ## Patch pod from JSON can change image
  # Command
  kubectl patch "${kube_flags[@]}" -f docs/admin/limitrange/valid-pod.yaml -p='{"spec":{"containers":[{"name": "kubernetes-serve-hostname", "image": "kubernetes/pause"}]}}'
  # Post-condition: valid-pod POD has image kubernetes/pause
  kube::test::get_object_assert pods "{{range.items}}{{$image_field}}:{{end}}" 'kubernetes/pause:'

  ## If resourceVersion is specified in the patch, it will be treated as a precondition, i.e., if the resourceVersion is different from that is stored in the server, the Patch should be rejected
  ERROR_FILE="${KUBE_TEMP}/conflict-error"
  ## If the resourceVersion is the same as the one stored in the server, the patch will be applied.
  # Command
  # Needs to retry because other party may change the resource.
  for count in $(seq 0 3); do
    resourceVersion=$(kubectl get "${kube_flags[@]}" pod valid-pod -o go-template='{{ .metadata.resourceVersion }}')
    kubectl patch "${kube_flags[@]}" pod valid-pod -p='{"spec":{"containers":[{"name": "kubernetes-serve-hostname", "image": "nginx"}]},"metadata":{"resourceVersion":"'$resourceVersion'"}}' 2> "${ERROR_FILE}" || true
    if grep -q "the object has been modified" "${ERROR_FILE}"; then
      kube::log::status "retry $1, error: $(cat ${ERROR_FILE})"
      rm "${ERROR_FILE}"
      sleep $((2**count))
    else
      rm "${ERROR_FILE}"
      kube::test::get_object_assert pods "{{range.items}}{{$image_field}}:{{end}}" 'nginx:'
      break
    fi
  done

  ## If the resourceVersion is the different from the one stored in the server, the patch will be rejected.
  resourceVersion=$(kubectl get "${kube_flags[@]}" pod valid-pod -o go-template='{{ .metadata.resourceVersion }}')
  ((resourceVersion+=100))
  # Command
  kubectl patch "${kube_flags[@]}" pod valid-pod -p='{"spec":{"containers":[{"name": "kubernetes-serve-hostname", "image": "nginx"}]},"metadata":{"resourceVersion":"'$resourceVersion'"}}' 2> "${ERROR_FILE}" || true
  # Post-condition: should get an error reporting the conflict
  if grep -q "please apply your changes to the latest version and try again" "${ERROR_FILE}"; then
    kube::log::status "\"kubectl patch with resourceVersion $resourceVersion\" returns error as expected: $(cat ${ERROR_FILE})"
  else
    kube::log::status "\"kubectl patch with resourceVersion $resourceVersion\" returns unexpected error or non-error: $(cat ${ERROR_FILE})"
    exit 1
  fi
  rm "${ERROR_FILE}"

  ## --force replace pod can change other field, e.g., spec.container.name
  # Command
  kubectl get "${kube_flags[@]}" pod valid-pod -o json | sed 's/"kubernetes-serve-hostname"/"replaced-k8s-serve-hostname"/g' > /tmp/tmp-valid-pod.json
  kubectl replace "${kube_flags[@]}" --force -f /tmp/tmp-valid-pod.json
  # Post-condition: spec.container.name = "replaced-k8s-serve-hostname"
  kube::test::get_object_assert 'pod valid-pod' "{{(index .spec.containers 0).name}}" 'replaced-k8s-serve-hostname'
  #cleaning
  rm /tmp/tmp-valid-pod.json
  
  ## replace of a cluster scoped resource can succeed
  # Pre-condition: a node exists
  kubectl create -f - "${kube_flags[@]}" << __EOF__
{
  "kind": "Node",
  "apiVersion": "v1",
  "metadata": {
    "name": "node-${version}-test"
  }
}
__EOF__
  kubectl replace -f - "${kube_flags[@]}" << __EOF__
{
  "kind": "Node",
  "apiVersion": "v1",
  "metadata": {
    "name": "node-${version}-test",
    "annotations": {"a":"b"}
  }
}
__EOF__
  # Post-condition: the node command succeeds
  kube::test::get_object_assert "node node-${version}-test" "{{.metadata.annotations.a}}" 'b'
  kubectl delete node node-${version}-test

  ## kubectl edit can update the image field of a POD. tmp-editor.sh is a fake editor
  echo -e '#!/bin/bash\nsed -i "s/nginx/gcr.io\/google_containers\/serve_hostname/g" $1' > /tmp/tmp-editor.sh
  chmod +x /tmp/tmp-editor.sh
  EDITOR=/tmp/tmp-editor.sh ${KUBE_OUTPUT_HOSTBIN}/kubectl edit "${kube_flags[@]}" pods/valid-pod
  # Post-condition: valid-pod POD has image gcr.io/google_containers/serve_hostname
  kube::test::get_object_assert pods "{{range.items}}{{$image_field}}:{{end}}" 'gcr.io/google_containers/serve_hostname:'
  # cleaning
  rm /tmp/tmp-editor.sh
  [ "$(EDITOR=cat kubectl edit pod/valid-pod 2>&1 | grep 'Edit cancelled')" ]
  [ "$(EDITOR=cat kubectl edit pod/valid-pod | grep 'name: valid-pod')" ]
  [ "$(EDITOR=cat kubectl edit --windows-line-endings pod/valid-pod | file - | grep CRLF)" ]
  [ ! "$(EDITOR=cat kubectl edit --windows-line-endings=false pod/valid-pod | file - | grep CRLF)" ]

  ### Overwriting an existing label is not permitted
  # Pre-condition: name is valid-pod
  kube::test::get_object_assert 'pod valid-pod' "{{${labels_field}.name}}" 'valid-pod'
  # Command
  ! kubectl label pods valid-pod name=valid-pod-super-sayan "${kube_flags[@]}"
  # Post-condition: name is still valid-pod
  kube::test::get_object_assert 'pod valid-pod' "{{${labels_field}.name}}" 'valid-pod'

  ### --overwrite must be used to overwrite existing label, can be applied to all resources
  # Pre-condition: name is valid-pod
  kube::test::get_object_assert 'pod valid-pod' "{{${labels_field}.name}}" 'valid-pod'
  # Command
  kubectl label --overwrite pods --all name=valid-pod-super-sayan "${kube_flags[@]}"
  # Post-condition: name is valid-pod-super-sayan
  kube::test::get_object_assert 'pod valid-pod' "{{${labels_field}.name}}" 'valid-pod-super-sayan'

  ### Delete POD by label
  # Pre-condition: valid-pod POD exists
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'
  # Command
  kubectl delete pods -l'name in (valid-pod-super-sayan)' --grace-period=0 "${kube_flags[@]}"
  # Post-condition: valid-pod POD doesn't exist
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''

  ### Create two PODs from 1 yaml file
  # Pre-condition: no POD exists
  create_and_use_new_namespace
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create -f docs/user-guide/multi-pod.yaml "${kube_flags[@]}"
  # Post-condition: valid-pod and redis-proxy PODs exist
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'redis-master:redis-proxy:'

  ### Delete two PODs from 1 yaml file
  # Pre-condition: redis-master and redis-proxy PODs exist
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'redis-master:redis-proxy:'
  # Command
  kubectl delete -f docs/user-guide/multi-pod.yaml "${kube_flags[@]}"
  # Post-condition: no PODs exist
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''

  ## kubectl apply should update configuration annotations only if apply is already called
  ## 1. kubectl create doesn't set the annotation
  # Pre-Condition: no POD exists
  create_and_use_new_namespace
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command: create a pod "test-pod"
  kubectl create -f hack/testdata/pod.yaml "${kube_flags[@]}"
  # Post-Condition: pod "test-pod" is created
  kube::test::get_object_assert 'pods test-pod' "{{${labels_field}.name}}" 'test-pod-label'
  # Post-Condition: pod "test-pod" doesn't have configuration annotation
  ! [[ "$(kubectl get pods test-pod -o yaml "${kube_flags[@]}" | grep kubectl.kubernetes.io/last-applied-configuration)" ]]
  ## 2. kubectl replace doesn't set the annotation
  kubectl get pods test-pod -o yaml "${kube_flags[@]}" | sed 's/test-pod-label/test-pod-replaced/g' > "${KUBE_TEMP}"/test-pod-replace.yaml
  # Command: replace the pod "test-pod"
  kubectl replace -f "${KUBE_TEMP}"/test-pod-replace.yaml "${kube_flags[@]}"
  # Post-Condition: pod "test-pod" is replaced
  kube::test::get_object_assert 'pods test-pod' "{{${labels_field}.name}}" 'test-pod-replaced'
  # Post-Condition: pod "test-pod" doesn't have configuration annotation
  ! [[ "$(kubectl get pods test-pod -o yaml "${kube_flags[@]}" | grep kubectl.kubernetes.io/last-applied-configuration)" ]]
  ## 3. kubectl apply does set the annotation
  # Command: apply the pod "test-pod"
  kubectl apply -f hack/testdata/pod-apply.yaml "${kube_flags[@]}"
  # Post-Condition: pod "test-pod" is applied
  kube::test::get_object_assert 'pods test-pod' "{{${labels_field}.name}}" 'test-pod-applied'
  # Post-Condition: pod "test-pod" has configuration annotation
  [[ "$(kubectl get pods test-pod -o yaml "${kube_flags[@]}" | grep kubectl.kubernetes.io/last-applied-configuration)" ]]
  kubectl get pods test-pod -o yaml "${kube_flags[@]}" | grep kubectl.kubernetes.io/last-applied-configuration > "${KUBE_TEMP}"/annotation-configuration
  ## 4. kubectl replace updates an existing annotation
  kubectl get pods test-pod -o yaml "${kube_flags[@]}" | sed 's/test-pod-applied/test-pod-replaced/g' > "${KUBE_TEMP}"/test-pod-replace.yaml
  # Command: replace the pod "test-pod"
  kubectl replace -f "${KUBE_TEMP}"/test-pod-replace.yaml "${kube_flags[@]}"
  # Post-Condition: pod "test-pod" is replaced
  kube::test::get_object_assert 'pods test-pod' "{{${labels_field}.name}}" 'test-pod-replaced'
  # Post-Condition: pod "test-pod" has configuration annotation, and it's updated (different from the annotation when it's applied)
  [[ "$(kubectl get pods test-pod -o yaml "${kube_flags[@]}" | grep kubectl.kubernetes.io/last-applied-configuration)" ]]
  kubectl get pods test-pod -o yaml "${kube_flags[@]}" | grep kubectl.kubernetes.io/last-applied-configuration > "${KUBE_TEMP}"/annotation-configuration-replaced
  ! [[ $(diff -q "${KUBE_TEMP}"/annotation-configuration "${KUBE_TEMP}"/annotation-configuration-replaced > /dev/null) ]]
  # Clean up
  rm "${KUBE_TEMP}"/test-pod-replace.yaml "${KUBE_TEMP}"/annotation-configuration "${KUBE_TEMP}"/annotation-configuration-replaced
  kubectl delete pods test-pod "${kube_flags[@]}"

  ## Configuration annotations should be set when --save-config is enabled
  ## 1. kubectl create --save-config should generate configuration annotation
  # Pre-Condition: no POD exists
  create_and_use_new_namespace
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command: create a pod "test-pod"
  kubectl create -f hack/testdata/pod.yaml --save-config "${kube_flags[@]}"
  # Post-Condition: pod "test-pod" has configuration annotation
  [[ "$(kubectl get pods test-pod -o yaml "${kube_flags[@]}" | grep kubectl.kubernetes.io/last-applied-configuration)" ]]
  # Clean up
  kubectl delete -f hack/testdata/pod.yaml "${kube_flags[@]}"
  ## 2. kubectl edit --save-config should generate configuration annotation
  # Pre-Condition: no POD exists, then create pod "test-pod", which shouldn't have configuration annotation
  create_and_use_new_namespace
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''
  kubectl create -f hack/testdata/pod.yaml "${kube_flags[@]}"
  ! [[ "$(kubectl get pods test-pod -o yaml "${kube_flags[@]}" | grep kubectl.kubernetes.io/last-applied-configuration)" ]]
  # Command: edit the pod "test-pod"
  temp_editor="${KUBE_TEMP}/tmp-editor.sh"
  echo -e '#!/bin/bash\nsed -i "s/test-pod-label/test-pod-label-edited/g" $@' > "${temp_editor}"
  chmod +x "${temp_editor}"
  EDITOR=${temp_editor} kubectl edit pod test-pod --save-config "${kube_flags[@]}"
  # Post-Condition: pod "test-pod" has configuration annotation
  [[ "$(kubectl get pods test-pod -o yaml "${kube_flags[@]}" | grep kubectl.kubernetes.io/last-applied-configuration)" ]]
  # Clean up
  kubectl delete -f hack/testdata/pod.yaml "${kube_flags[@]}"
  ## 3. kubectl replace --save-config should generate configuration annotation
  # Pre-Condition: no POD exists, then create pod "test-pod", which shouldn't have configuration annotation
  create_and_use_new_namespace
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''
  kubectl create -f hack/testdata/pod.yaml "${kube_flags[@]}"
  ! [[ "$(kubectl get pods test-pod -o yaml "${kube_flags[@]}" | grep kubectl.kubernetes.io/last-applied-configuration)" ]]
  # Command: replace the pod "test-pod"
  kubectl replace -f hack/testdata/pod.yaml --save-config "${kube_flags[@]}"
  # Post-Condition: pod "test-pod" has configuration annotation
  [[ "$(kubectl get pods test-pod -o yaml "${kube_flags[@]}" | grep kubectl.kubernetes.io/last-applied-configuration)" ]]
  # Clean up
  kubectl delete -f hack/testdata/pod.yaml "${kube_flags[@]}"
  ## 4. kubectl run --save-config should generate configuration annotation
  # Pre-Condition: no RC exists
  kube::test::get_object_assert rc "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command: create the rc "nginx" with image nginx
  kubectl run nginx --image=nginx --save-config --generator=run/v1 "${kube_flags[@]}"
  # Post-Condition: rc "nginx" has configuration annotation
  [[ "$(kubectl get rc nginx -o yaml "${kube_flags[@]}" | grep kubectl.kubernetes.io/last-applied-configuration)" ]]
  ## 5. kubectl expose --save-config should generate configuration annotation
  # Pre-Condition: no service exists
  kube::test::get_object_assert svc "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command: expose the rc "nginx"
  kubectl expose rc nginx --save-config --port=80 --target-port=8000 "${kube_flags[@]}"
  # Post-Condition: service "nginx" has configuration annotation
  [[ "$(kubectl get svc nginx -o yaml "${kube_flags[@]}" | grep kubectl.kubernetes.io/last-applied-configuration)" ]]
  # Clean up
  kubectl delete rc,svc nginx
  ## 6. kubectl autoscale --save-config should generate configuration annotation
  # Pre-Condition: no RC exists, then create the rc "frontend", which shouldn't have configuration annotation
  kube::test::get_object_assert rc "{{range.items}}{{$id_field}}:{{end}}" ''
  kubectl create -f hack/testdata/frontend-controller.yaml "${kube_flags[@]}"
  ! [[ "$(kubectl get rc frontend -o yaml "${kube_flags[@]}" | grep kubectl.kubernetes.io/last-applied-configuration)" ]]
  # Command: autoscale rc "frontend"
  kubectl autoscale -f hack/testdata/frontend-controller.yaml --save-config "${kube_flags[@]}" --max=2
  # Post-Condition: hpa "frontend" has configuration annotation
  [[ "$(kubectl get hpa.extensions frontend -o yaml "${kube_flags[@]}" | grep kubectl.kubernetes.io/last-applied-configuration)" ]]
  # Clean up
  # Note that we should delete hpa first, otherwise it may fight with the rc reaper.
  kubectl delete hpa frontend "${kube_flags[@]}"
  kubectl delete rc  frontend "${kube_flags[@]}"

  ## kubectl apply should create the resource that doesn't exist yet
  # Pre-Condition: no POD exists
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command: apply a pod "test-pod" (doesn't exist) should create this pod
  kubectl apply -f hack/testdata/pod.yaml "${kube_flags[@]}"
  # Post-Condition: pod "test-pod" is created
  kube::test::get_object_assert 'pods test-pod' "{{${labels_field}.name}}" 'test-pod-label'
  # Post-Condition: pod "test-pod" has configuration annotation
  [[ "$(kubectl get pods test-pod -o yaml "${kube_flags[@]}" | grep kubectl.kubernetes.io/last-applied-configuration)" ]]
  # Clean up
  kubectl delete pods test-pod "${kube_flags[@]}"

  ## kubectl run should create deployments or jobs
  # Pre-Condition: no Job exists
  kube::test::get_object_assert jobs "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl run pi --generator=job/v1beta1 --image=perl --restart=OnFailure -- perl -Mbignum=bpi -wle 'print bpi(20)' "${kube_flags[@]}"
  # Post-Condition: Job "pi" is created
  kube::test::get_object_assert jobs "{{range.items}}{{$id_field}}:{{end}}" 'pi:'
  # Clean up
  kubectl delete jobs pi "${kube_flags[@]}"
  # Post-condition: no pods exist.
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''
  # Pre-Condition: no Deployment exists
  kube::test::get_object_assert deployment "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl run nginx --image=nginx --generator=deployment/v1beta1 "${kube_flags[@]}"
  # Post-Condition: Deployment "nginx" is created
  kube::test::get_object_assert deployment "{{range.items}}{{$id_field}}:{{end}}" 'nginx:'
  # Clean up
  kubectl delete deployment nginx "${kube_flags[@]}"

  ##############
  # Namespaces #
  ##############

  ### Create a new namespace
  # Pre-condition: only the "default" namespace exists
  # The Pre-condition doesn't hold anymore after we create and switch namespaces before creating pods with same name in the test.
  # kube::test::get_object_assert namespaces "{{range.items}}{{$id_field}}:{{end}}" 'default:'
  # Command
  kubectl create namespace my-namespace
  # Post-condition: namespace 'my-namespace' is created.
  kube::test::get_object_assert 'namespaces/my-namespace' "{{$id_field}}" 'my-namespace'
  # Clean up
  kubectl delete namespace my-namespace

  ##############
  # Pods in Namespaces #
  ##############

  ### Create a new namespace
  # Pre-condition: the other namespace does not exist
  kube::test::get_object_assert 'namespaces' '{{range.items}}{{ if eq $id_field \"other\" }}found{{end}}{{end}}:' ':'
  # Command
  kubectl create namespace other
  # Post-condition: namespace 'other' is created.
  kube::test::get_object_assert 'namespaces/other' "{{$id_field}}" 'other'

  ### Create POD valid-pod in specific namespace
  # Pre-condition: no POD exists
  kube::test::get_object_assert 'pods --namespace=other' "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create "${kube_flags[@]}" --namespace=other -f docs/admin/limitrange/valid-pod.yaml
  # Post-condition: valid-pod POD is created
  kube::test::get_object_assert 'pods --namespace=other' "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'

  ### Delete POD valid-pod in specific namespace
  # Pre-condition: valid-pod POD exists
  kube::test::get_object_assert 'pods --namespace=other' "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'
  # Command
  kubectl delete "${kube_flags[@]}" pod --namespace=other valid-pod --grace-period=0
  # Post-condition: valid-pod POD doesn't exist
  kube::test::get_object_assert 'pods --namespace=other' "{{range.items}}{{$id_field}}:{{end}}" ''
  # Clean up
  kubectl delete namespace other

  ##############
  # Secrets #
  ##############

  ### Create a new namespace
  # Pre-condition: the test-secrets namespace does not exist
  kube::test::get_object_assert 'namespaces' '{{range.items}}{{ if eq $id_field \"test-secrets\" }}found{{end}}{{end}}:' ':'
  # Command
  kubectl create namespace test-secrets
  # Post-condition: namespace 'test-secrets' is created.
  kube::test::get_object_assert 'namespaces/test-secrets' "{{$id_field}}" 'test-secrets'

  ### Create a generic secret in a specific namespace
  # Pre-condition: no SECRET exists
  kube::test::get_object_assert 'secrets --namespace=test-secrets' "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create secret generic test-secret --from-literal=key1=value1 --type=test-type --namespace=test-secrets
  # Post-condition: secret exists and has expected values
  kube::test::get_object_assert 'secret/test-secret --namespace=test-secrets' "{{$id_field}}" 'test-secret'
  kube::test::get_object_assert 'secret/test-secret --namespace=test-secrets' "{{$secret_type}}" 'test-type'
  [[ "$(kubectl get secret/test-secret --namespace=test-secrets -o yaml "${kube_flags[@]}" | grep 'key1: dmFsdWUx')" ]]
  # Clean-up
  kubectl delete secret test-secret --namespace=test-secrets

  ### Create a docker-registry secret in a specific namespace
  # Pre-condition: no SECRET exists
  kube::test::get_object_assert 'secrets --namespace=test-secrets' "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create secret docker-registry test-secret --docker-username=test-user --docker-password=test-password --docker-email='test-user@test.com' --namespace=test-secrets
  # Post-condition: secret exists and has expected values
  kube::test::get_object_assert 'secret/test-secret --namespace=test-secrets' "{{$id_field}}" 'test-secret'
  kube::test::get_object_assert 'secret/test-secret --namespace=test-secrets' "{{$secret_type}}" 'kubernetes.io/dockercfg'
  [[ "$(kubectl get secret/test-secret --namespace=test-secrets -o yaml "${kube_flags[@]}" | grep '.dockercfg:')" ]]
  # Clean-up
  kubectl delete secret test-secret --namespace=test-secrets

  ### Create a secret using output flags
  # Pre-condition: no secret exists
  kube::test::get_object_assert 'secrets --namespace=test-secrets' "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  [[ "$(kubectl create secret generic test-secret --namespace=test-secrets --from-literal=key1=value1 --output=go-template --template=\"{{.metadata.name}}:\" | grep 'test-secret:')" ]]
  ## Clean-up
  kubectl delete secret test-secret --namespace=test-secrets  
  # Clean up
  kubectl delete namespace test-secrets

  ######################
  # ConfigMap          #
  ######################

  kubectl create -f docs/user-guide/configmap/configmap.yaml
  kube::test::get_object_assert configmap "{{range.items}}{{$id_field}}{{end}}" 'test-configmap'
  kubectl delete configmap test-configmap "${kube_flags[@]}"

  ### Create a new namespace
  # Pre-condition: the test-configmaps namespace does not exist
  kube::test::get_object_assert 'namespaces' '{{range.items}}{{ if eq $id_field \"test-configmaps\" }}found{{end}}{{end}}:' ':'
  # Command
  kubectl create namespace test-configmaps
  # Post-condition: namespace 'test-configmaps' is created.
  kube::test::get_object_assert 'namespaces/test-configmaps' "{{$id_field}}" 'test-configmaps'

  ### Create a generic configmap in a specific namespace
  # Pre-condition: no configmaps namespace exists
  kube::test::get_object_assert 'configmaps --namespace=test-configmaps' "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create configmap test-configmap --from-literal=key1=value1 --namespace=test-configmaps
  # Post-condition: configmap exists and has expected values
  kube::test::get_object_assert 'configmap/test-configmap --namespace=test-configmaps' "{{$id_field}}" 'test-configmap'
  [[ "$(kubectl get configmap/test-configmap --namespace=test-configmaps -o yaml "${kube_flags[@]}" | grep 'key1: value1')" ]]
  # Clean-up
  kubectl delete configmap test-configmap --namespace=test-configmaps
  kubectl delete namespace test-configmaps
  
  ####################
  # Service Accounts #
  ####################

  ### Create a new namespace
  # Pre-condition: the test-service-accounts namespace does not exist
  kube::test::get_object_assert 'namespaces' '{{range.items}}{{ if eq $id_field \"test-service-accounts\" }}found{{end}}{{end}}:' ':'
  # Command
  kubectl create namespace test-service-accounts
  # Post-condition: namespace 'test-service-accounts' is created.
  kube::test::get_object_assert 'namespaces/test-service-accounts' "{{$id_field}}" 'test-service-accounts'

  ### Create a service account in a specific namespace
  # Command
  kubectl create serviceaccount test-service-account --namespace=test-service-accounts
  # Post-condition: secret exists and has expected values
  kube::test::get_object_assert 'serviceaccount/test-service-account --namespace=test-service-accounts' "{{$id_field}}" 'test-service-account'
  # Clean-up
  kubectl delete serviceaccount test-service-account --namespace=test-service-accounts
  # Clean up
  kubectl delete namespace test-service-accounts

  #################
  # Pod templates #
  #################

  ### Create PODTEMPLATE
  # Pre-condition: no PODTEMPLATE
  kube::test::get_object_assert podtemplates "{{range.items}}{{.metadata.name}}:{{end}}" ''
  # Command
  kubectl create -f docs/user-guide/walkthrough/podtemplate.json "${kube_flags[@]}"
  # Post-condition: nginx PODTEMPLATE is available
  kube::test::get_object_assert podtemplates "{{range.items}}{{.metadata.name}}:{{end}}" 'nginx:'

  ### Printing pod templates works
  kubectl get podtemplates "${kube_flags[@]}"
  [[ "$(kubectl get podtemplates -o yaml "${kube_flags[@]}" | grep nginx)" ]]

  ### Delete nginx pod template by name
  # Pre-condition: nginx pod template is available
  kube::test::get_object_assert podtemplates "{{range.items}}{{.metadata.name}}:{{end}}" 'nginx:'
  # Command
  kubectl delete podtemplate nginx "${kube_flags[@]}"
  # Post-condition: No templates exist
  kube::test::get_object_assert podtemplate "{{range.items}}{{.metadata.name}}:{{end}}" ''


  ############
  # Services #
  ############
  # switch back to the default namespace
  kubectl config set-context "${CONTEXT}" --namespace=""
  kube::log::status "Testing kubectl(${version}:services)"

  ### Create redis-master service from JSON
  # Pre-condition: Only the default kubernetes services exist
  kube::test::get_object_assert services "{{range.items}}{{$id_field}}:{{end}}" 'kubernetes:'
  # Command
  kubectl create -f examples/guestbook/redis-master-service.yaml "${kube_flags[@]}"
  # Post-condition: redis-master service exists
  kube::test::get_object_assert services "{{range.items}}{{$id_field}}:{{end}}" 'kubernetes:redis-master:'
  # Describe command should print detailed information
  kube::test::describe_object_assert services 'redis-master' "Name:" "Labels:" "Selector:" "IP:" "Port:" "Endpoints:" "Session Affinity:"
  # Describe command (resource only) should print detailed information
  kube::test::describe_resource_assert services "Name:" "Labels:" "Selector:" "IP:" "Port:" "Endpoints:" "Session Affinity:"

  ### Dump current redis-master service
  output_service=$(kubectl get service redis-master -o json --output-version=v1 "${kube_flags[@]}")

  ### Delete redis-master-service by id
  # Pre-condition: redis-master service exists
  kube::test::get_object_assert services "{{range.items}}{{$id_field}}:{{end}}" 'kubernetes:redis-master:'
  # Command
  kubectl delete service redis-master "${kube_flags[@]}"
  # Post-condition: Only the default kubernetes services exist
  kube::test::get_object_assert services "{{range.items}}{{$id_field}}:{{end}}" 'kubernetes:'

  ### Create redis-master-service from dumped JSON
  # Pre-condition: Only the default kubernetes services exist
  kube::test::get_object_assert services "{{range.items}}{{$id_field}}:{{end}}" 'kubernetes:'
  # Command
  echo "${output_service}" | kubectl create -f - "${kube_flags[@]}"
  # Post-condition: redis-master service is created
  kube::test::get_object_assert services "{{range.items}}{{$id_field}}:{{end}}" 'kubernetes:redis-master:'

  ### Create redis-master-${version}-test service
  # Pre-condition: redis-master-service service exists
  kube::test::get_object_assert services "{{range.items}}{{$id_field}}:{{end}}" 'kubernetes:redis-master:'
  # Command
  kubectl create -f - "${kube_flags[@]}" << __EOF__
{
  "kind": "Service",
  "apiVersion": "v1",
  "metadata": {
    "name": "service-${version}-test"
  },
  "spec": {
    "ports": [
      {
        "protocol": "TCP",
        "port": 80,
        "targetPort": 80
      }
    ]
  }
}
__EOF__
  # Post-condition: service-${version}-test service is created
  kube::test::get_object_assert services "{{range.items}}{{$id_field}}:{{end}}" 'kubernetes:redis-master:service-.*-test:'

  ### Identity
  kubectl get service "${kube_flags[@]}" service-${version}-test -o json | kubectl replace "${kube_flags[@]}" -f -

  ### Delete services by id
  # Pre-condition: service-${version}-test exists
  kube::test::get_object_assert services "{{range.items}}{{$id_field}}:{{end}}" 'kubernetes:redis-master:service-.*-test:'
  # Command
  kubectl delete service redis-master "${kube_flags[@]}"
  kubectl delete service "service-${version}-test" "${kube_flags[@]}"
  # Post-condition: Only the default kubernetes services exist
  kube::test::get_object_assert services "{{range.items}}{{$id_field}}:{{end}}" 'kubernetes:'

  ### Create two services
  # Pre-condition: Only the default kubernetes services exist
  kube::test::get_object_assert services "{{range.items}}{{$id_field}}:{{end}}" 'kubernetes:'
  # Command
  kubectl create -f examples/guestbook/redis-master-service.yaml "${kube_flags[@]}"
  kubectl create -f examples/guestbook/redis-slave-service.yaml "${kube_flags[@]}"
  # Post-condition: redis-master and redis-slave services are created
  kube::test::get_object_assert services "{{range.items}}{{$id_field}}:{{end}}" 'kubernetes:redis-master:redis-slave:'

  ### Custom columns can be specified
  # Pre-condition: generate output using custom columns
  output_message=$(kubectl get services -o=custom-columns=NAME:.metadata.name,RSRC:.metadata.resourceVersion 2>&1 "${kube_flags[@]}")
  # Post-condition: should contain name column
  kube::test::if_has_string "${output_message}" 'redis-master'

  ### Delete multiple services at once
  # Pre-condition: redis-master and redis-slave services exist
  kube::test::get_object_assert services "{{range.items}}{{$id_field}}:{{end}}" 'kubernetes:redis-master:redis-slave:'
  # Command
  kubectl delete services redis-master redis-slave "${kube_flags[@]}" # delete multiple services at once
  # Post-condition: Only the default kubernetes services exist
  kube::test::get_object_assert services "{{range.items}}{{$id_field}}:{{end}}" 'kubernetes:'


  ###########################
  # Replication controllers #
  ###########################

  kube::log::status "Testing kubectl(${version}:replicationcontrollers)"

  ### Create and stop controller, make sure it doesn't leak pods
  # Pre-condition: no replication controller exists
  kube::test::get_object_assert rc "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create -f hack/testdata/frontend-controller.yaml "${kube_flags[@]}"
  kubectl delete rc frontend "${kube_flags[@]}"
  # Post-condition: no pods from frontend controller
  kube::test::get_object_assert 'pods -l "name=frontend"' "{{range.items}}{{$id_field}}:{{end}}" ''

  ### Create replication controller frontend from JSON
  # Pre-condition: no replication controller exists
  kube::test::get_object_assert rc "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create -f hack/testdata/frontend-controller.yaml "${kube_flags[@]}"
  # Post-condition: frontend replication controller is created
  kube::test::get_object_assert rc "{{range.items}}{{$id_field}}:{{end}}" 'frontend:'
  # Describe command should print detailed information
  kube::test::describe_object_assert rc 'frontend' "Name:" "Image(s):" "Labels:" "Selector:" "Replicas:" "Pods Status:"
  # Describe command (resource only) should print detailed information
  kube::test::describe_resource_assert rc "Name:" "Name:" "Image(s):" "Labels:" "Selector:" "Replicas:" "Pods Status:"

  ### Scale replication controller frontend with current-replicas and replicas
  # Pre-condition: 3 replicas
  kube::test::get_object_assert 'rc frontend' "{{$rc_replicas_field}}" '3'
  # Command
  kubectl scale --current-replicas=3 --replicas=2 replicationcontrollers frontend "${kube_flags[@]}"
  # Post-condition: 2 replicas
  kube::test::get_object_assert 'rc frontend' "{{$rc_replicas_field}}" '2'

  ### Scale replication controller frontend with (wrong) current-replicas and replicas
  # Pre-condition: 2 replicas
  kube::test::get_object_assert 'rc frontend' "{{$rc_replicas_field}}" '2'
  # Command
  ! kubectl scale --current-replicas=3 --replicas=2 replicationcontrollers frontend "${kube_flags[@]}"
  # Post-condition: nothing changed
  kube::test::get_object_assert 'rc frontend' "{{$rc_replicas_field}}" '2'

  ### Scale replication controller frontend with replicas only
  # Pre-condition: 2 replicas
  kube::test::get_object_assert 'rc frontend' "{{$rc_replicas_field}}" '2'
  # Command
  kubectl scale  --replicas=3 replicationcontrollers frontend "${kube_flags[@]}"
  # Post-condition: 3 replicas
  kube::test::get_object_assert 'rc frontend' "{{$rc_replicas_field}}" '3'

  ### Scale replication controller from JSON with replicas only
  # Pre-condition: 3 replicas
  kube::test::get_object_assert 'rc frontend' "{{$rc_replicas_field}}" '3'
  # Command
  kubectl scale  --replicas=2 -f hack/testdata/frontend-controller.yaml "${kube_flags[@]}"
  # Post-condition: 2 replicas
  kube::test::get_object_assert 'rc frontend' "{{$rc_replicas_field}}" '2'
  # Clean-up
  kubectl delete rc frontend "${kube_flags[@]}"

  ### Scale multiple replication controllers
  kubectl create -f examples/guestbook/redis-master-controller.yaml "${kube_flags[@]}"
  kubectl create -f examples/guestbook/redis-slave-controller.yaml "${kube_flags[@]}"
  # Command
  kubectl scale rc/redis-master rc/redis-slave --replicas=4 "${kube_flags[@]}"
  # Post-condition: 4 replicas each
  kube::test::get_object_assert 'rc redis-master' "{{$rc_replicas_field}}" '4'
  kube::test::get_object_assert 'rc redis-slave' "{{$rc_replicas_field}}" '4'
  # Clean-up
  kubectl delete rc redis-{master,slave} "${kube_flags[@]}"

  ### Scale a job
  kubectl create -f docs/user-guide/job.yaml "${kube_flags[@]}"
  # Command
  kubectl scale --replicas=2 job/pi
  # Post-condition: 2 replicas for pi
  kube::test::get_object_assert 'job pi' "{{$job_parallelism_field}}" '2'
  # Clean-up
  kubectl delete job/pi "${kube_flags[@]}"

  ### Scale a deployment
  kubectl create -f docs/user-guide/deployment.yaml "${kube_flags[@]}"
  # Command
  kubectl scale --current-replicas=3 --replicas=1 deployment/nginx-deployment
  # Post-condition: 1 replica for nginx-deployment
  kube::test::get_object_assert 'deployment nginx-deployment' "{{$deployment_replicas}}" '1'
  # Clean-up
  kubectl delete deployment/nginx-deployment "${kube_flags[@]}"

  ### Expose a deployment as a service
  kubectl create -f docs/user-guide/deployment.yaml "${kube_flags[@]}"
  # Pre-condition: 3 replicas
  kube::test::get_object_assert 'deployment nginx-deployment' "{{$deployment_replicas}}" '3'
  # Command
  kubectl expose deployment/nginx-deployment
  # Post-condition: service exists and exposes deployment port (80)
  kube::test::get_object_assert 'service nginx-deployment' "{{$port_field}}" '80'
  # Clean-up
  kubectl delete deployment/nginx-deployment service/nginx-deployment "${kube_flags[@]}"

  ### Expose replication controller as service
  kubectl create -f hack/testdata/frontend-controller.yaml "${kube_flags[@]}"
  # Pre-condition: 3 replicas
  kube::test::get_object_assert 'rc frontend' "{{$rc_replicas_field}}" '3'
  # Command
  kubectl expose rc frontend --port=80 "${kube_flags[@]}"
  # Post-condition: service exists and the port is unnamed
  kube::test::get_object_assert 'service frontend' "{{$port_name}} {{$port_field}}" '<no value> 80'
  # Command
  kubectl expose service frontend --port=443 --name=frontend-2 "${kube_flags[@]}"
  # Post-condition: service exists and the port is unnamed
  kube::test::get_object_assert 'service frontend-2' "{{$port_name}} {{$port_field}}" '<no value> 443'
  # Command
  kubectl create -f docs/admin/limitrange/valid-pod.yaml "${kube_flags[@]}"
  kubectl expose pod valid-pod --port=444 --name=frontend-3 "${kube_flags[@]}"
  # Post-condition: service exists and the port is unnamed
  kube::test::get_object_assert 'service frontend-3' "{{$port_name}} {{$port_field}}" '<no value> 444'
  # Create a service using service/v1 generator
  kubectl expose rc frontend --port=80 --name=frontend-4 --generator=service/v1 "${kube_flags[@]}"
  # Post-condition: service exists and the port is named default.
  kube::test::get_object_assert 'service frontend-4' "{{$port_name}} {{$port_field}}" 'default 80'
  # Verify that expose service works without specifying a port.
  kubectl expose service frontend --name=frontend-5 "${kube_flags[@]}"
  # Post-condition: service exists with the same port as the original service.
  kube::test::get_object_assert 'service frontend-5' "{{$port_field}}" '80'
  # Cleanup services
  kubectl delete pod valid-pod "${kube_flags[@]}"
  kubectl delete service frontend{,-2,-3,-4,-5} "${kube_flags[@]}"

  ### Expose negative invalid resource test
  # Pre-condition: don't need
  # Command
  output_message=$(! kubectl expose nodes 127.0.0.1 2>&1 "${kube_flags[@]}")
  # Post-condition: the error message has "cannot expose" string
  kube::test::if_has_string "${output_message}" 'cannot expose'

  ### Try to generate a service with invalid name (exceeding maximum valid size)
  # Pre-condition: use --name flag
  output_message=$(! kubectl expose -f hack/testdata/pod-with-large-name.yaml --name=invalid-large-service-name --port=8081 2>&1 "${kube_flags[@]}")
  # Post-condition: should fail due to invalid name
  kube::test::if_has_string "${output_message}" 'metadata.name: Invalid value'
  # Pre-condition: default run without --name flag; should succeed by truncating the inherited name
  output_message=$(kubectl expose -f hack/testdata/pod-with-large-name.yaml --port=8081 2>&1 "${kube_flags[@]}")
  # Post-condition: inherited name from pod has been truncated
  kube::test::if_has_string "${output_message}" '\"kubernetes-serve-hostnam\" exposed'
  # Clean-up
  kubectl delete svc kubernetes-serve-hostnam "${kube_flags[@]}"

  ### Expose multiport object as a new service
  # Pre-condition: don't use --port flag
  output_message=$(kubectl expose -f docs/admin/high-availability/etcd.yaml --selector=test=etcd 2>&1 "${kube_flags[@]}")
  # Post-condition: expose succeeded
  kube::test::if_has_string "${output_message}" '\"etcd-server\" exposed'
  # Post-condition: generated service has both ports from the exposed pod
  kube::test::get_object_assert 'service etcd-server' "{{$port_name}} {{$port_field}}" 'port-1 2380'
  kube::test::get_object_assert 'service etcd-server' "{{$second_port_name}} {{$second_port_field}}" 'port-2 4001'
  # Clean-up
  kubectl delete svc etcd-server "${kube_flags[@]}"

  ### Delete replication controller with id
  # Pre-condition: frontend replication controller exists
  kube::test::get_object_assert rc "{{range.items}}{{$id_field}}:{{end}}" 'frontend:'
  # Command
  kubectl delete rc frontend "${kube_flags[@]}"
  # Post-condition: no replication controller exists
  kube::test::get_object_assert rc "{{range.items}}{{$id_field}}:{{end}}" ''

  ### Create two replication controllers
  # Pre-condition: no replication controller exists
  kube::test::get_object_assert rc "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create -f hack/testdata/frontend-controller.yaml "${kube_flags[@]}"
  kubectl create -f examples/guestbook/redis-slave-controller.yaml "${kube_flags[@]}"
  # Post-condition: frontend and redis-slave
  kube::test::get_object_assert rc "{{range.items}}{{$id_field}}:{{end}}" 'frontend:redis-slave:'

  ### Delete multiple controllers at once
  # Pre-condition: frontend and redis-slave
  kube::test::get_object_assert rc "{{range.items}}{{$id_field}}:{{end}}" 'frontend:redis-slave:'
  # Command
  kubectl delete rc frontend redis-slave "${kube_flags[@]}" # delete multiple controllers at once
  # Post-condition: no replication controller exists
  kube::test::get_object_assert rc "{{range.items}}{{$id_field}}:{{end}}" ''

  ### Auto scale replication controller
  # Pre-condition: no replication controller exists
  kube::test::get_object_assert rc "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create -f hack/testdata/frontend-controller.yaml "${kube_flags[@]}"
  kube::test::get_object_assert rc "{{range.items}}{{$id_field}}:{{end}}" 'frontend:'
  # autoscale 1~2 pods, CPU utilization 70%, rc specified by file
  kubectl autoscale -f hack/testdata/frontend-controller.yaml "${kube_flags[@]}" --max=2 --cpu-percent=70
  kube::test::get_object_assert 'hpa frontend' "{{$hpa_min_field}} {{$hpa_max_field}} {{$hpa_cpu_field}}" '1 2 70'
  kubectl delete hpa frontend "${kube_flags[@]}"
  # autoscale 2~3 pods, default CPU utilization (80%), rc specified by name
  kubectl autoscale rc frontend "${kube_flags[@]}" --min=2 --max=3
  kube::test::get_object_assert 'hpa frontend' "{{$hpa_min_field}} {{$hpa_max_field}} {{$hpa_cpu_field}}" '2 3 80'
  kubectl delete hpa frontend "${kube_flags[@]}"
  # autoscale without specifying --max should fail
  ! kubectl autoscale rc frontend "${kube_flags[@]}"
  # Clean up
  kubectl delete rc frontend "${kube_flags[@]}"

  ### Auto scale deployment
  # Pre-condition: no deployment exists
  kube::test::get_object_assert deployment "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create -f docs/user-guide/deployment.yaml "${kube_flags[@]}"
  kube::test::get_object_assert deployment "{{range.items}}{{$id_field}}:{{end}}" 'nginx-deployment:'
  # autoscale 2~3 pods, default CPU utilization (80%)
  kubectl-with-retry autoscale deployment nginx-deployment "${kube_flags[@]}" --min=2 --max=3
  kube::test::get_object_assert 'hpa.extensions nginx-deployment' "{{$hpa_min_field}} {{$hpa_max_field}} {{$hpa_cpu_field}}" '2 3 80'
  # Clean up
  # Note that we should delete hpa first, otherwise it may fight with the deployment reaper.
  kubectl delete hpa nginx-deployment "${kube_flags[@]}"
  kubectl delete deployment.extensions nginx-deployment "${kube_flags[@]}"

  ### Rollback a deployment
  # Pre-condition: no deployment exists
  kube::test::get_object_assert deployment "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  # Create a deployment (revision 1)
  kubectl create -f docs/user-guide/deployment.yaml "${kube_flags[@]}"
  kube::test::get_object_assert deployment "{{range.items}}{{$id_field}}:{{end}}" 'nginx-deployment:'
  kube::test::get_object_assert deployment "{{range.items}}{{$deployment_image_field}}:{{end}}" 'nginx:'
  # Rollback to revision 1 - should be no-op
  kubectl rollout undo deployment nginx-deployment --to-revision=1 "${kube_flags[@]}"
  kube::test::get_object_assert deployment "{{range.items}}{{$deployment_image_field}}:{{end}}" 'nginx:'
  # Update the deployment (revision 2)
  kubectl apply -f hack/testdata/deployment-revision2.yaml "${kube_flags[@]}"
  kube::test::get_object_assert deployment.extensions "{{range.items}}{{$deployment_image_field}}:{{end}}" 'nginx:latest:'
  # Rollback to revision 1
  kubectl rollout undo deployment nginx-deployment --to-revision=1 "${kube_flags[@]}"
  sleep 1
  kube::test::get_object_assert deployment "{{range.items}}{{$deployment_image_field}}:{{end}}" 'nginx:'
  # Rollback to revision 1000000 - should be no-op
  kubectl rollout undo deployment nginx-deployment --to-revision=1000000 "${kube_flags[@]}"
  kube::test::get_object_assert deployment "{{range.items}}{{$deployment_image_field}}:{{end}}" 'nginx:'
  # Rollback to last revision
  kubectl rollout undo deployment nginx-deployment "${kube_flags[@]}"
  sleep 1
  kube::test::get_object_assert deployment "{{range.items}}{{$deployment_image_field}}:{{end}}" 'nginx:latest:'
  # Clean up
  kubectl delete deployment nginx-deployment "${kube_flags[@]}"
  kubectl delete rs -l pod-template-hash "${kube_flags[@]}"


  ######################
  # Replica Sets       #
  ######################

  kube::log::status "Testing kubectl(${version}:replicasets)"

  ### Create and stop a replica set, make sure it doesn't leak pods
  # Pre-condition: no replica set exists
  kube::test::get_object_assert rs "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create -f docs/user-guide/replicaset/frontend.yaml "${kube_flags[@]}"
  kubectl delete rs frontend "${kube_flags[@]}"
  # Post-condition: no pods from frontend replica set
  kube::test::get_object_assert 'pods -l "tier=frontend"' "{{range.items}}{{$id_field}}:{{end}}" ''

  ### Create replica set frontend from YAML
  # Pre-condition: no replica set exists
  kube::test::get_object_assert rs "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create -f docs/user-guide/replicaset/frontend.yaml "${kube_flags[@]}"
  # Post-condition: frontend replica set is created
  kube::test::get_object_assert rs "{{range.items}}{{$id_field}}:{{end}}" 'frontend:'

  # TODO(madhusudancs): Add describe tests once PR #20886 that implements describe for ReplicaSet is merged.

  ### Scale replica set frontend with current-replicas and replicas
  # Pre-condition: 3 replicas
  kube::test::get_object_assert 'rs frontend' "{{$rs_replicas_field}}" '3'
  # Command
  kubectl scale --current-replicas=3 --replicas=2 replicasets frontend "${kube_flags[@]}"
  # Post-condition: 2 replicas
  kube::test::get_object_assert 'rs frontend' "{{$rs_replicas_field}}" '2'
  # Clean-up
  kubectl delete rs frontend "${kube_flags[@]}"

  # TODO(madhusudancs): Fix this when Scale group issues are resolved (see issue #18528).

  ### Expose replica set as service
  kubectl create -f docs/user-guide/replicaset/frontend.yaml "${kube_flags[@]}"
  # Pre-condition: 3 replicas
  kube::test::get_object_assert 'rs frontend' "{{$rs_replicas_field}}" '3'
  # Command
  kubectl expose rs frontend --port=80 "${kube_flags[@]}"
  # Post-condition: service exists and the port is unnamed
  kube::test::get_object_assert 'service frontend' "{{$port_name}} {{$port_field}}" '<no value> 80'
  # Create a service using service/v1 generator
  kubectl expose rs frontend --port=80 --name=frontend-2 --generator=service/v1 "${kube_flags[@]}"
  # Post-condition: service exists and the port is named default.
  kube::test::get_object_assert 'service frontend-2' "{{$port_name}} {{$port_field}}" 'default 80'
  # Cleanup services
  kubectl delete service frontend{,-2} "${kube_flags[@]}"

  ### Delete replica set with id
  # Pre-condition: frontend replica set exists
  kube::test::get_object_assert rs "{{range.items}}{{$id_field}}:{{end}}" 'frontend:'
  # Command
  kubectl delete rs frontend "${kube_flags[@]}"
  # Post-condition: no replica set exists
  kube::test::get_object_assert rs "{{range.items}}{{$id_field}}:{{end}}" ''

  ### Create two replica sets
  # Pre-condition: no replica set exists
  kube::test::get_object_assert rs "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create -f docs/user-guide/replicaset/frontend.yaml "${kube_flags[@]}"
  kubectl create -f docs/user-guide/replicaset/redis-slave.yaml "${kube_flags[@]}"
  # Post-condition: frontend and redis-slave
  kube::test::get_object_assert rs "{{range.items}}{{$id_field}}:{{end}}" 'frontend:redis-slave:'

  ### Delete multiple replica sets at once
  # Pre-condition: frontend and redis-slave
  kube::test::get_object_assert rs "{{range.items}}{{$id_field}}:{{end}}" 'frontend:redis-slave:'
  # Command
  kubectl delete rs frontend redis-slave "${kube_flags[@]}" # delete multiple replica sets at once
  # Post-condition: no replica set exists
  kube::test::get_object_assert rs "{{range.items}}{{$id_field}}:{{end}}" ''

  ######################
  # Multiple Resources #
  ######################

  kube::log::status "Testing kubectl(${version}:multiple resources)"

  FILES="hack/testdata/multi-resource-yaml
  hack/testdata/multi-resource-list
  hack/testdata/multi-resource-json
  hack/testdata/multi-resource-rclist
  hack/testdata/multi-resource-svclist"
  YAML=".yaml"
  JSON=".json"
  for file in $FILES; do
    if [ -f $file$YAML ]
    then
      file=$file$YAML
      replace_file="${file%.yaml}-modify.yaml"
    else
      file=$file$JSON
      replace_file="${file%.json}-modify.json"
    fi

    has_svc=true
    has_rc=true
    two_rcs=false
    two_svcs=false
    if [[ "${file}" == *rclist* ]]; then
      has_svc=false
      two_rcs=true
    fi
    if [[ "${file}" == *svclist* ]]; then
      has_rc=false
      two_svcs=true
    fi

    ### Create, get, describe, replace, label, annotate, and then delete service nginxsvc and replication controller my-nginx from 5 types of files:
    ### 1) YAML, separated by ---; 2) JSON, with a List type; 3) JSON, with JSON object concatenation
    ### 4) JSON, with a ReplicationControllerList type; 5) JSON, with a ServiceList type
    echo "Testing with file ${file} and replace with file ${replace_file}"
    # Pre-condition: no service (other than default kubernetes services) or replication controller exists
    kube::test::get_object_assert services "{{range.items}}{{$id_field}}:{{end}}" 'kubernetes:'
    kube::test::get_object_assert rc "{{range.items}}{{$id_field}}:{{end}}" ''
    # Command
    kubectl create -f "${file}" "${kube_flags[@]}"
    # Post-condition: mock service (and mock2) exists
    if [ "$has_svc" = true ]; then
      if [ "$two_svcs" = true ]; then
        kube::test::get_object_assert services "{{range.items}}{{$id_field}}:{{end}}" 'kubernetes:mock:mock2:'
      else
        kube::test::get_object_assert services "{{range.items}}{{$id_field}}:{{end}}" 'kubernetes:mock:'
      fi
    fi
    # Post-condition: mock rc (and mock2) exists
    if [ "$has_rc" = true ]; then
      if [ "$two_rcs" = true ]; then
        kube::test::get_object_assert rc "{{range.items}}{{$id_field}}:{{end}}" 'mock:mock2:'
      else
        kube::test::get_object_assert rc "{{range.items}}{{$id_field}}:{{end}}" 'mock:'
      fi
    fi
    # Command
    kubectl get -f "${file}" "${kube_flags[@]}"
    # Command: watching multiple resources should return "not supported" error
    WATCH_ERROR_FILE="${KUBE_TEMP}/kubectl-watch-error"
    kubectl get -f "${file}" "${kube_flags[@]}" "--watch" 2> ${WATCH_ERROR_FILE} || true
    if ! grep -q "watch is only supported on individual resources and resource collections" "${WATCH_ERROR_FILE}"; then
      kube::log::error_exit "kubectl watch multiple resource returns unexpected error or non-error: $(cat ${WATCH_ERROR_FILE})" "1"
    fi
    kubectl describe -f "${file}" "${kube_flags[@]}"
    # Command
    kubectl replace -f $replace_file --force "${kube_flags[@]}"
    # Post-condition: mock service (and mock2) and mock rc (and mock2) are replaced
    if [ "$has_svc" = true ]; then
      kube::test::get_object_assert 'services mock' "{{${labels_field}.status}}" 'replaced'
      if [ "$two_svcs" = true ]; then
        kube::test::get_object_assert 'services mock2' "{{${labels_field}.status}}" 'replaced'
      fi
    fi
    if [ "$has_rc" = true ]; then
      kube::test::get_object_assert 'rc mock' "{{${labels_field}.status}}" 'replaced'
      if [ "$two_rcs" = true ]; then
        kube::test::get_object_assert 'rc mock2' "{{${labels_field}.status}}" 'replaced'
      fi
    fi
    # Command: kubectl edit multiple resources
    temp_editor="${KUBE_TEMP}/tmp-editor.sh"
    echo -e '#!/bin/bash\nsed -i "s/status\:\ replaced/status\:\ edited/g" $@' > "${temp_editor}"
    chmod +x "${temp_editor}"
    EDITOR="${temp_editor}" kubectl edit "${kube_flags[@]}" -f "${file}"
    # Post-condition: mock service (and mock2) and mock rc (and mock2) are edited
    if [ "$has_svc" = true ]; then
      kube::test::get_object_assert 'services mock' "{{${labels_field}.status}}" 'edited'
      if [ "$two_svcs" = true ]; then
        kube::test::get_object_assert 'services mock2' "{{${labels_field}.status}}" 'edited'
      fi
    fi
    if [ "$has_rc" = true ]; then
      kube::test::get_object_assert 'rc mock' "{{${labels_field}.status}}" 'edited'
      if [ "$two_rcs" = true ]; then
        kube::test::get_object_assert 'rc mock2' "{{${labels_field}.status}}" 'edited'
      fi
    fi
    # cleaning
    rm "${temp_editor}"
    # Command
    # We need to set --overwrite, because otherwise, if the first attempt to run "kubectl label"
    # fails on some, but not all, of the resources, retries will fail because it tries to modify
    # existing labels.
    kubectl-with-retry label -f $file labeled=true --overwrite "${kube_flags[@]}"
    # Post-condition: mock service and mock rc (and mock2) are labeled
    if [ "$has_svc" = true ]; then
      kube::test::get_object_assert 'services mock' "{{${labels_field}.labeled}}" 'true'
      if [ "$two_svcs" = true ]; then
        kube::test::get_object_assert 'services mock2' "{{${labels_field}.labeled}}" 'true'
      fi
    fi
    if [ "$has_rc" = true ]; then
      kube::test::get_object_assert 'rc mock' "{{${labels_field}.labeled}}" 'true'
      if [ "$two_rcs" = true ]; then
        kube::test::get_object_assert 'rc mock2' "{{${labels_field}.labeled}}" 'true'
      fi
    fi
    # Command
    # Command
    # We need to set --overwrite, because otherwise, if the first attempt to run "kubectl annotate"
    # fails on some, but not all, of the resources, retries will fail because it tries to modify
    # existing annotations.
    kubectl-with-retry annotate -f $file annotated=true --overwrite "${kube_flags[@]}"
    # Post-condition: mock service (and mock2) and mock rc (and mock2) are annotated
    if [ "$has_svc" = true ]; then
      kube::test::get_object_assert 'services mock' "{{${annotations_field}.annotated}}" 'true'
      if [ "$two_svcs" = true ]; then
        kube::test::get_object_assert 'services mock2' "{{${annotations_field}.annotated}}" 'true'
      fi
    fi
    if [ "$has_rc" = true ]; then
      kube::test::get_object_assert 'rc mock' "{{${annotations_field}.annotated}}" 'true'
      if [ "$two_rcs" = true ]; then
        kube::test::get_object_assert 'rc mock2' "{{${annotations_field}.annotated}}" 'true'
      fi
    fi
    # Cleanup resources created
    kubectl delete -f "${file}" "${kube_flags[@]}"
  done

  #############################
  # Multiple Resources via URL#
  #############################

  # Pre-condition: no service (other than default kubernetes services) or replication controller exists
  kube::test::get_object_assert services "{{range.items}}{{$id_field}}:{{end}}" 'kubernetes:'
  kube::test::get_object_assert rc "{{range.items}}{{$id_field}}:{{end}}" ''

  # Command
  kubectl create -f https://raw.githubusercontent.com/kubernetes/kubernetes/master/hack/testdata/multi-resource-yaml.yaml "${kube_flags[@]}"

  # Post-condition: service(mock) and rc(mock) exist
  kube::test::get_object_assert services "{{range.items}}{{$id_field}}:{{end}}" 'kubernetes:mock:'
  kube::test::get_object_assert rc "{{range.items}}{{$id_field}}:{{end}}" 'mock:'

  # Clean up
  kubectl delete -f https://raw.githubusercontent.com/kubernetes/kubernetes/master/hack/testdata/multi-resource-yaml.yaml "${kube_flags[@]}"

  # Post-condition: no service (other than default kubernetes services) or replication controller exists
  kube::test::get_object_assert services "{{range.items}}{{$id_field}}:{{end}}" 'kubernetes:'
  kube::test::get_object_assert rc "{{range.items}}{{$id_field}}:{{end}}" ''


  ######################
  # Persistent Volumes #
  ######################

  ### Create and delete persistent volume examples
  # Pre-condition: no persistent volumes currently exist
  kube::test::get_object_assert pv "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create -f docs/user-guide/persistent-volumes/volumes/local-01.yaml "${kube_flags[@]}"
  kube::test::get_object_assert pv "{{range.items}}{{$id_field}}:{{end}}" 'pv0001:'
  kubectl delete pv pv0001 "${kube_flags[@]}"
  kubectl create -f docs/user-guide/persistent-volumes/volumes/local-02.yaml "${kube_flags[@]}"
  kube::test::get_object_assert pv "{{range.items}}{{$id_field}}:{{end}}" 'pv0002:'
  kubectl delete pv pv0002 "${kube_flags[@]}"
  kubectl create -f docs/user-guide/persistent-volumes/volumes/gce.yaml "${kube_flags[@]}"
  kube::test::get_object_assert pv "{{range.items}}{{$id_field}}:{{end}}" 'pv0003:'
  kubectl delete pv pv0003 "${kube_flags[@]}"
  # Post-condition: no PVs
  kube::test::get_object_assert pv "{{range.items}}{{$id_field}}:{{end}}" ''

  ############################
  # Persistent Volume Claims #
  ############################

  ### Create and delete persistent volume claim examples
  # Pre-condition: no persistent volume claims currently exist
  kube::test::get_object_assert pvc "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create -f docs/user-guide/persistent-volumes/claims/claim-01.yaml "${kube_flags[@]}"
  kube::test::get_object_assert pvc "{{range.items}}{{$id_field}}:{{end}}" 'myclaim-1:'
  kubectl delete pvc myclaim-1 "${kube_flags[@]}"

  kubectl create -f docs/user-guide/persistent-volumes/claims/claim-02.yaml "${kube_flags[@]}"
  kube::test::get_object_assert pvc "{{range.items}}{{$id_field}}:{{end}}" 'myclaim-2:'
  kubectl delete pvc myclaim-2 "${kube_flags[@]}"

  kubectl create -f docs/user-guide/persistent-volumes/claims/claim-03.json "${kube_flags[@]}"
  kube::test::get_object_assert pvc "{{range.items}}{{$id_field}}:{{end}}" 'myclaim-3:'
  kubectl delete pvc myclaim-3 "${kube_flags[@]}"
  # Post-condition: no PVCs
  kube::test::get_object_assert pvc "{{range.items}}{{$id_field}}:{{end}}" ''



  #########
  # Nodes #
  #########

  kube::log::status "Testing kubectl(${version}:nodes)"

  kube::test::get_object_assert nodes "{{range.items}}{{$id_field}}:{{end}}" '127.0.0.1:'

  kube::test::describe_object_assert nodes "127.0.0.1" "Name:" "Labels:" "CreationTimestamp:" "Conditions:" "Addresses:" "Capacity:" "Pods:"
  # Describe command (resource only) should print detailed information
  kube::test::describe_resource_assert nodes "Name:" "Labels:" "CreationTimestamp:" "Conditions:" "Addresses:" "Capacity:" "Pods:"

  ### kubectl patch update can mark node unschedulable
  # Pre-condition: node is schedulable
  kube::test::get_object_assert "nodes 127.0.0.1" "{{.spec.unschedulable}}" '<no value>'
  kubectl patch "${kube_flags[@]}" nodes "127.0.0.1" -p='{"spec":{"unschedulable":true}}'
  # Post-condition: node is unschedulable
  kube::test::get_object_assert "nodes 127.0.0.1" "{{.spec.unschedulable}}" 'true'
  kubectl patch "${kube_flags[@]}" nodes "127.0.0.1" -p='{"spec":{"unschedulable":null}}'
  # Post-condition: node is schedulable
  kube::test::get_object_assert "nodes 127.0.0.1" "{{.spec.unschedulable}}" '<no value>'


  #####################
  # Retrieve multiple #
  #####################

  kube::log::status "Testing kubectl(${version}:multiget)"
  kube::test::get_object_assert 'nodes/127.0.0.1 service/kubernetes' "{{range.items}}{{$id_field}}:{{end}}" '127.0.0.1:kubernetes:'


  #####################
  # Resource aliasing #
  #####################

  kube::log::status "Testing resource aliasing"
  kubectl create -f examples/cassandra/cassandra.yaml "${kube_flags[@]}"
  kubectl create -f examples/cassandra/cassandra-service.yaml "${kube_flags[@]}"
  kube::test::get_object_assert "all -l'app=cassandra'" "{{range.items}}{{range .metadata.labels}}{{.}}:{{end}}{{end}}" 'cassandra:cassandra:'
  kubectl delete all -l app=cassandra "${kube_flags[@]}"


  ###########
  # Explain #
  ###########

  kube::log::status "Testing kubectl(${version}:explain)"
  kubectl explain pods
  # shortcuts work
  kubectl explain po
  kubectl explain po.status.message


  ###########
  # Swagger #
  ###########

  if [[ -n "${version}" ]]; then
    # Verify schema
    file="${KUBE_TEMP}/schema-${version}.json"
    curl -s "http://127.0.0.1:${API_PORT}/swaggerapi/api/${version}" > "${file}"
    [[ "$(grep "list of returned" "${file}")" ]]
    [[ "$(grep "List of pods" "${file}")" ]]
    [[ "$(grep "Watch for changes to the described resources" "${file}")" ]]
  fi

  #####################
  # Kubectl --sort-by #
  #####################

  ### sort-by should not panic if no pod exists
  # Pre-condition: no POD exists
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl get pods --sort-by="{metadata.name}"

  ############################
  # Kubectl --all-namespaces #
  ############################

  # Pre-condition: the "default" namespace exists
  kube::test::get_object_assert namespaces "{{range.items}}{{if eq $id_field \\\"default\\\"}}{{$id_field}}:{{end}}{{end}}" 'default:'

  ### Create POD
  # Pre-condition: no POD exists
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''
  # Command
  kubectl create "${kube_flags[@]}" -f docs/admin/limitrange/valid-pod.yaml
  # Post-condition: valid-pod is created
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'

  ### Verify a specific namespace is ignored when all-namespaces is provided
  # Command
  kubectl get pods --all-namespaces --namespace=default

  ### Clean up
  # Pre-condition: valid-pod exists
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" 'valid-pod:'
  # Command
  kubectl delete "${kube_flags[@]}" pod valid-pod --grace-period=0
  # Post-condition: valid-pod doesn't exist
  kube::test::get_object_assert pods "{{range.items}}{{$id_field}}:{{end}}" ''

  kube::test::clear_all
}

kube_api_versions=(
  ""
  v1
)
for version in "${kube_api_versions[@]}"; do
  KUBE_API_VERSIONS="v1,autoscaling/v1,batch/v1,extensions/v1beta1" runTests "${version}"
done

kube::log::status "TEST PASSED"
