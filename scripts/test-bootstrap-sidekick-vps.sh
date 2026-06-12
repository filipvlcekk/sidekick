#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${REPO_ROOT}/scripts/bootstrap-sidekick-vps.sh"

tmp_file="$(mktemp)"
trap 'rm -f "${tmp_file}"' EXIT

append_if_missing "alpha" "${tmp_file}"
append_if_missing "alpha" "${tmp_file}"
append_if_missing "beta" "${tmp_file}"

expected=$'alpha\nbeta'
actual="$(cat "${tmp_file}")"

if [[ "${actual}" != "${expected}" ]]; then
  echo "append_if_missing produced unexpected content"
  echo "expected:"
  printf '%s\n' "${expected}"
  echo "actual:"
  printf '%s\n' "${actual}"
  exit 1
fi

DOCKER_APT_ARCH=amd64 DOCKER_APT_CODENAME=noble repo_line="$(docker_apt_repo_line)"
expected_repo_line='deb [arch=amd64 signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu noble stable'
if [[ "${repo_line}" != "${expected_repo_line}" ]]; then
  echo "docker_apt_repo_line produced unexpected content"
  echo "expected: ${expected_repo_line}"
  echo "actual:   ${repo_line}"
  exit 1
fi

plugin_path="$(compose_plugin_install_path)"
if [[ "${plugin_path}" != "/usr/local/lib/docker/cli-plugins/docker-compose" ]]; then
  echo "compose_plugin_install_path produced unexpected path: ${plugin_path}"
  exit 1
fi

COMPOSE_PLUGIN_ARCH=x86_64 plugin_url="$(compose_plugin_download_url)"
expected_plugin_url='https://github.com/docker/compose/releases/download/v2.39.2/docker-compose-linux-x86_64'
if [[ "${plugin_url}" != "${expected_plugin_url}" ]]; then
  echo "compose_plugin_download_url produced unexpected url"
  echo "expected: ${expected_plugin_url}"
  echo "actual:   ${plugin_url}"
  exit 1
fi

flags="$(extract_lsattr_flags '----ia--------e------- /home/admin/.ssh/authorized_keys')"
if [[ "${flags}" != "----ia--------e-------" ]]; then
  echo "extract_lsattr_flags produced unexpected flags: ${flags}"
  exit 1
fi

if ! path_has_restrictive_attr_flags "${flags}"; then
  echo "path_has_restrictive_attr_flags should detect immutable or append-only flags"
  exit 1
fi

if path_has_restrictive_attr_flags "----------------------"; then
  echo "path_has_restrictive_attr_flags incorrectly reported restrictive flags"
  exit 1
fi

echo "bootstrap-sidekick-vps smoke test passed"
