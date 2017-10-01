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

function prepare-package-manager() {
  echo "Prepare package manager"

  # Useful if a mirror is broken or slow
  echo "fastestmirror=True" >> /etc/dnf/dnf.conf

  # In Fedora 23, installed version does not work with Salt
  # Cf. https://github.com/saltstack/salt/issues/31001
  dnf update -y dnf dnf-plugins-core
}

function write-salt-config() {
  local role="$1"

  # Update salt configuration
  mkdir -p /etc/salt/minion.d

  mkdir -p /srv/salt-overlay/pillar
  cat <<EOF >/srv/salt-overlay/pillar/cluster-params.sls
service_cluster_ip_range: '$(echo "$SERVICE_CLUSTER_IP_RANGE" | sed -e "s/'/''/g")'
cert_ip: '$(echo "$MASTER_IP" | sed -e "s/'/''/g")'
enable_cluster_monitoring: '$(echo "$ENABLE_CLUSTER_MONITORING" | sed -e "s/'/''/g")'
enable_cluster_logging: '$(echo "$ENABLE_CLUSTER_LOGGING" | sed -e "s/'/''/g")'
enable_cluster_ui: '$(echo "$ENABLE_CLUSTER_UI" | sed -e "s/'/''/g")'
enable_node_logging: '$(echo "$ENABLE_NODE_LOGGING" | sed -e "s/'/''/g")'
logging_destination: '$(echo "$LOGGING_DESTINATION" | sed -e "s/'/''/g")'
elasticsearch_replicas: '$(echo "$ELASTICSEARCH_LOGGING_REPLICAS" | sed -e "s/'/''/g")'
enable_cluster_dns: '$(echo "$ENABLE_CLUSTER_DNS" | sed -e "s/'/''/g")'
dns_replicas: '$(echo "$DNS_REPLICAS" | sed -e "s/'/''/g")'
dns_server: '$(echo "$DNS_SERVER_IP" | sed -e "s/'/''/g")'
dns_domain: '$(echo "$DNS_DOMAIN" | sed -e "s/'/''/g")'
instance_prefix: '$(echo "$INSTANCE_PREFIX" | sed -e "s/'/''/g")'
admission_control: '$(echo "$ADMISSION_CONTROL" | sed -e "s/'/''/g")'
enable_cpu_cfs_quota: '$(echo "$ENABLE_CPU_CFS_QUOTA" | sed -e "s/'/''/g")'
network_provider: '$(echo "$NETWORK_PROVIDER" | sed -e "s/'/''/g")'
cluster_cidr: '$(echo "$CLUSTER_IP_RANGE" | sed -e "s/'/''/g")'
opencontrail_tag: '$(echo "$OPENCONTRAIL_TAG" | sed -e "s/'/''/g")'
opencontrail_kubernetes_tag: '$(echo "$OPENCONTRAIL_KUBERNETES_TAG" | sed -e "s/'/''/g")'
opencontrail_public_subnet: '$(echo "$OPENCONTRAIL_PUBLIC_SUBNET" | sed -e "s/'/''/g")'
e2e_storage_test_environment: '$(echo "$E2E_STORAGE_TEST_ENVIRONMENT" | sed -e "s/'/''/g")'
EOF

  cat <<EOF >/etc/salt/minion.d/log-level-debug.conf
log_level: warning
log_level_logfile: warning
EOF

  cat <<EOF >/etc/salt/minion.d/grains.conf
grains:
  node_ip: '$(echo "$MASTER_IP" | sed -e "s/'/''/g")'
  publicAddressOverride: '$(echo "$MASTER_IP" | sed -e "s/'/''/g")'
  network_mode: openvswitch
  networkInterfaceName: '$(echo "$NETWORK_IF_NAME" | sed -e "s/'/''/g")'
  api_servers: '$(echo "$MASTER_IP" | sed -e "s/'/''/g")'
  cloud: vagrant
  roles:
    - $role
  runtime_config: '$(echo "$RUNTIME_CONFIG" | sed -e "s/'/''/g")'
  docker_opts: '$(echo "$DOCKER_OPTS" | sed -e "s/'/''/g")'
  master_extra_sans: '$(echo "$MASTER_EXTRA_SANS" | sed -e "s/'/''/g")'
  keep_host_etcd: true
EOF
}

function install-salt() {
  server_binary_tar="/vagrant/server/kubernetes-server-linux-amd64.tar.gz"
  if [[ ! -f "$server_binary_tar" ]]; then
    server_binary_tar="/vagrant/_output/release-tars/kubernetes-server-linux-amd64.tar.gz"
  fi
  if [[ ! -f "$server_binary_tar" ]]; then
    release_not_found
  fi

  salt_tar="/vagrant/server/kubernetes-salt.tar.gz"
  if [[ ! -f "$salt_tar" ]]; then
    salt_tar="/vagrant/_output/release-tars/kubernetes-salt.tar.gz"
  fi
  if [[ ! -f "$salt_tar" ]]; then
    release_not_found
  fi

  echo "Running release install script"
  rm -rf /kube-install
  mkdir -p /kube-install
  pushd /kube-install
  tar xzf "$salt_tar"
  cp "$server_binary_tar" .
  ./kubernetes/saltbase/install.sh "${server_binary_tar##*/}"
  popd

  if ! which salt-call >/dev/null 2>&1; then
    # Install salt binaries
    curl -sS -L --connect-timeout 20 --retry 6 --retry-delay 10 https://bootstrap.saltstack.com | sh -s

    # Fedora >= 23 includes salt packages but the bootstrap is
    # creating configuration for a (non-existent) salt repo anyway.
    # Remove the invalid repo to prevent dnf from warning about it on
    # every update.  Assume this problem is specific to Fedora 23 and
    # will fixed by the time another version of Fedora lands.
    local fedora_version=$(grep 'VERSION_ID' /etc/os-release | sed 's+VERSION_ID=++')
    if [[ "${fedora_version}" = '23' ]]; then
      local repo_file='/etc/yum.repos.d/saltstack-salt-fedora-23.repo'
      if [[ -f "${repo_file}" ]]; then
        rm "${repo_file}"
      fi
    fi

  fi
}

function run-salt() {
  echo "  Now waiting for the Salt provisioning process to complete on this machine."
  echo "  This can take some time based on your network, disk, and cpu speed."
  salt-call --local state.highstate
}

function create-salt-kubelet-auth() {
  local -r kubelet_kubeconfig_folder="/srv/salt-overlay/salt/kubelet"
  mkdir -p "${kubelet_kubeconfig_folder}"
  (umask 077;
  cat > "${kubelet_kubeconfig_folder}/kubeconfig" << EOF
apiVersion: v1
kind: Config
clusters:
- cluster:
    insecure-skip-tls-verify: true
  name: local
contexts:
- context:
    cluster: local
    user: kubelet
  name: service-account-context
current-context: service-account-context
users:
- name: kubelet
  user:
    token: ${KUBELET_TOKEN}
EOF
  )
}

function create-salt-kubeproxy-auth() {
  kube_proxy_kubeconfig_folder="/srv/salt-overlay/salt/kube-proxy"
  mkdir -p "${kube_proxy_kubeconfig_folder}"
  (umask 077;
  cat > "${kube_proxy_kubeconfig_folder}/kubeconfig" << EOF
apiVersion: v1
kind: Config
clusters:
- cluster:
    insecure-skip-tls-verify: true
  name: local
contexts:
- context:
    cluster: local
    user: kube-proxy
  name: service-account-context
current-context: service-account-context
users:
- name: kube-proxy
  user:
    token: ${KUBE_PROXY_TOKEN}
EOF
  )
}
