#!/bin/bash

# Copyright 2015 The Kubernetes Authors All rights reserved.
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

set -o errexit
set -o nounset
set -o pipefail

KUBE_ROOT=$(dirname "${BASH_SOURCE}")/..
source "${KUBE_ROOT}/hack/lib/init.sh"

kube::golang::setup_env

generated_files=($(
  find . -not \( \
      \( \
        -wholename './output' \
        -o -wholename './_output' \
        -o -wholename './release' \
        -o -wholename './target' \
        -o -wholename '*/third_party/*' \
        -o -wholename '*/Godeps/*' \
        -o -wholename '*/codecgen-*-1234.generated.go' \
      \) -prune \
    \) -name '*.generated.go'))

# Register function to be called on EXIT to remove codecgen
# binary and also to touch the files that should be regenerated
# since they are first removed.
# This is necessary to make the script work after previous failure.
function cleanup {
  rm -f "${CODECGEN:-}"
  pushd "${KUBE_ROOT}" > /dev/null
  for (( i=0; i < number; i++ )); do
    touch "${generated_files[${i}]}" || true
  done
  popd > /dev/null
}
trap cleanup EXIT

# Sort all files in the dependency order.
number=${#generated_files[@]}
for (( i=0; i<number; i++ )); do
  visited[${i}]=false
done
result=""

# NOTE: depends function assumes that the whole repository is under
# */k8s.io/kubernetes directory - it will NOT work if that is not true.
function depends {
  file=${generated_files[$1]//\.generated\.go/.go}
  deps=$(go list -f "{{.Deps}}" ${file} | tr "[" " " | tr "]" " ")
  candidate=$(readlinkdashf "${generated_files[$2]//\.generated\.go/.go}")
  result=false
  for dep in ${deps}; do
    if [[ ${candidate} = *${dep} ]]; then
      result=true
    fi
  done
  echo ${result}
}

function tsort {
  visited[$1]=true
  local j=0
  for (( j=0; j<number; j++ )); do
    if ! ${visited[${j}]}; then
      if $(depends "$1" ${j}); then
        tsort $j
      fi
    fi
  done
  result="${result} $1"
}
for (( i=0; i<number; i++ )); do
  if ! ${visited[${i}]}; then
    tsort ${i}
  fi
done
index=(${result})

haveindex=${index:-}
if [[ -z ${haveindex} ]]; then
  echo No files found for $0
  echo A previous run of $0 may have deleted all the files and then crashed.
  echo Use 'touch' to create files named 'types.generated.go' listed as deleted in 'git status'
  exit 1
fi

CODECGEN="${PWD}/codecgen_binary"
godep go build -o "${CODECGEN}" github.com/ugorji/go/codec/codecgen

# Running codecgen fails if some of the files doesn't compile.
# Thus (since all the files are completely auto-generated and
# not required for the code to be compilable, we first remove
# them and the regenerate them.
for (( i=0; i < number; i++ )); do
  rm -f "${generated_files[${i}]}"
done

# Generate files in the dependency order.
for current in "${index[@]}"; do
  generated_file=${generated_files[${current}]}
  initial_dir=${PWD}
  file=${generated_file//\.generated\.go/.go}
  echo "codecgen processing ${file}"
  # codecgen work only if invoked from directory where the file
  # is located.
  pushd "$(dirname ${file})" > /dev/null
  base_file=$(basename "${file}")
  base_generated_file=$(basename "${generated_file}")
  # We use '-d 1234' flag to have a deterministic output every time.
  # The constant was just randomly chosen.
  echo Running ${CODECGEN} -d 1234 -o  "${base_generated_file}" "${base_file}"
  ${CODECGEN} -d 1234 -o "${base_generated_file}" "${base_file}"
  # Add boilerplate at the beginning of the generated file.
  sed 's/YEAR/2015/' "${initial_dir}/hack/boilerplate/boilerplate.go.txt" > "${base_generated_file}.tmp"
  cat "${base_generated_file}" >> "${base_generated_file}.tmp"
  mv "${base_generated_file}.tmp" "${base_generated_file}"
  echo "${generated_file} is regenerated."
  popd > /dev/null
done
