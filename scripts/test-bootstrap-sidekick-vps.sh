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

echo "bootstrap-sidekick-vps smoke test passed"
