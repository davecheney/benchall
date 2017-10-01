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

# Bring up a Kubernetes cluster.
#
# If the full release name (gs://<bucket>/<release>) is passed in then we take
# that directly.  If not then we assume we are doing development stuff and take
# the defaults in the release config.

set -o errexit
set -o nounset
set -o pipefail

KUBE_ROOT=$(dirname "${BASH_SOURCE}")/..
EXIT_ON_WEAK_ERROR="${EXIT_ON_WEAK_ERROR:-false}"

if [ -f "${KUBE_ROOT}/cluster/env.sh" ]; then
    source "${KUBE_ROOT}/cluster/env.sh"
fi

source "${KUBE_ROOT}/cluster/kube-env.sh"
source "${KUBE_ROOT}/cluster/kube-util.sh"


if [ -z "${ZONE-}" ]; then
  echo "... Starting cluster using provider: ${KUBERNETES_PROVIDER}" >&2
else
  echo "... Starting cluster in ${ZONE} using provider ${KUBERNETES_PROVIDER}" >&2
fi

echo "... calling verify-prereqs" >&2
verify-prereqs

if [[ "${KUBE_STAGE_IMAGES:-}" == "true" ]]; then
  echo "... staging images" >&2
  stage-images
fi

echo "... calling kube-up" >&2
kube-up

echo "... calling validate-cluster" >&2
# Override errexit
(validate-cluster) && validate_result="$?" || validate_result="$?"

# We have two different failure modes from validate cluster:
# - 1: fatal error - cluster won't be working correctly
# - 2: weak error - something went wrong, but cluster probably will be working correctly
# We always exit in case 1), but if EXIT_ON_WEAK_ERROR != true, then we don't fail on 2).
if [[ "${validate_result}" == "1" ]]; then
	exit 1
elif [[ "${validate_result}" == "2" ]]; then
	if [[ "${EXIT_ON_WEAK_ERROR}" == "true" ]]; then
		exit 1;
	else
		echo "...ignoring non-fatal errors in validate-cluster" >&2
	fi
fi

echo -e "Done, listing cluster services:\n" >&2
"${KUBE_ROOT}/cluster/kubectl.sh" cluster-info
echo

exit 0
