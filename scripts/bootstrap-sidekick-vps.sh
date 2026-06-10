#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

append_if_missing() {
  local line="$1"
  local file="$2"

  touch "${file}"
  if ! grep -Fqx "${line}" "${file}" 2>/dev/null; then
    printf '%s\n' "${line}" >> "${file}"
  fi
}

log_section() {
  echo
  echo "==> $1"
}

require_non_root_user() {
  if [[ "${EUID}" -eq 0 ]]; then
    echo "Error: do not run this script as root."
    exit 1
  fi
}

require_repo_root() {
  if [[ ! -f "${REPO_ROOT}/go.mod" ]]; then
    echo "Error: could not locate repo root from ${BASH_SOURCE[0]}."
    exit 1
  fi
}

prompt_server_ip() {
  if [[ -n "${SERVER_IP:-}" ]]; then
    return
  fi

  read -r -p "Enter this server's public IPv4 address: " SERVER_IP
  if [[ -z "${SERVER_IP}" ]]; then
    echo "Error: server IP is required."
    exit 1
  fi
}

install_system_packages() {
  log_section "Installing system packages"
  sudo apt-get update
  sudo apt-get install -y age ca-certificates curl docker.io git openssh-client snapd
  sudo systemctl enable --now docker
  sudo usermod -aG docker "${USER}" || true
}

install_go() {
  log_section "Installing Go"
  sudo systemctl enable --now snapd.socket
  if snap list go >/dev/null 2>&1; then
    sudo snap refresh go --classic
  else
    sudo snap install go --classic
  fi

  export PATH="${PATH}:/snap/bin"
  GOTOOLCHAIN=local go version >/dev/null
}

install_sops() {
  log_section "Installing SOPS"
  if command -v sops >/dev/null 2>&1; then
    echo "SOPS is already installed."
    return
  fi

  local sops_version="v3.9.0"
  local tmp_file
  tmp_file="$(mktemp)"
  curl -fsSL "https://github.com/getsops/sops/releases/download/${sops_version}/sops-${sops_version}.linux.amd64" -o "${tmp_file}"
  sudo mv "${tmp_file}" /usr/local/bin/sops
  sudo chmod +x /usr/local/bin/sops
}

build_and_install_sidekick() {
  log_section "Building and installing sidekick"
  (
    cd "${REPO_ROOT}"
    GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}" \
    GOTOOLCHAIN=local \
    go build -o sidekick .
  )
  sudo mv "${REPO_ROOT}/sidekick" /usr/local/bin/sidekick
  sudo chmod +x /usr/local/bin/sidekick
}

prepare_sidekick_config() {
  log_section "Preparing sidekick config directory"
  mkdir -p "${HOME}/.config/sidekick"
}

ensure_local_ssh_key() {
  log_section "Ensuring local SSH key exists"
  mkdir -p "${HOME}/.ssh"
  chmod 700 "${HOME}/.ssh"

  if [[ ! -f "${HOME}/.ssh/id_ed25519" ]]; then
    ssh-keygen -t ed25519 -N "" -f "${HOME}/.ssh/id_ed25519"
  fi

  local public_key
  public_key="$(cat "${HOME}/.ssh/id_ed25519.pub")"
  append_if_missing "${public_key}" "${HOME}/.ssh/authorized_keys"
  chmod 600 "${HOME}/.ssh/authorized_keys"
}

ensure_known_host() {
  log_section "Adding server host key to known_hosts"
  mkdir -p "${HOME}/.ssh"
  chmod 700 "${HOME}/.ssh"
  touch "${HOME}/.ssh/known_hosts"
  chmod 644 "${HOME}/.ssh/known_hosts"
  if ! ssh-keygen -F "${SERVER_IP}" >/dev/null 2>&1; then
    ssh-keyscan "${SERVER_IP}" >> "${HOME}/.ssh/known_hosts" 2>/dev/null
  fi
}

ensure_sidekick_user() {
  log_section "Ensuring sidekick user exists"
  if ! id sidekick >/dev/null 2>&1; then
    sudo useradd -m -s /bin/bash -G sudo sidekick
  fi

  echo "sidekick ALL=(ALL) NOPASSWD: ALL" | sudo tee /etc/sudoers.d/sidekick >/dev/null

  sudo mkdir -p /home/sidekick/.ssh
  sudo cp "${HOME}/.ssh/authorized_keys" /home/sidekick/.ssh/authorized_keys
  sudo chown -R sidekick:sidekick /home/sidekick/.ssh
  sudo chmod 700 /home/sidekick/.ssh
  sudo chmod 600 /home/sidekick/.ssh/authorized_keys
}

run_self_checks() {
  log_section "Running readiness checks"

  local failures=()
  local ready=()
  local ssh_check_output=""

  if command -v sidekick >/dev/null 2>&1; then
    ready+=("sidekick binary installed")
  else
    failures+=("sidekick binary is not installed")
  fi

  if command -v docker >/dev/null 2>&1; then
    ready+=("docker installed")
  else
    failures+=("docker is not installed")
  fi

  if command -v age >/dev/null 2>&1; then
    ready+=("age installed")
  else
    failures+=("age is not installed")
  fi

  if command -v sops >/dev/null 2>&1; then
    ready+=("sops installed")
  else
    failures+=("sops is not installed")
  fi

  if ssh_check_output="$(ssh -o BatchMode=yes -o ConnectTimeout=5 -o PreferredAuthentications=publickey "sidekick@${SERVER_IP}" 'whoami' 2>/dev/null)" && [[ "${ssh_check_output}" == "sidekick" ]]; then
    ready+=("public-key SSH login to sidekick@${SERVER_IP} works")
  else
    failures+=("public-key SSH login to sidekick@${SERVER_IP} failed")
  fi

  if [[ -S "${SSH_AUTH_SOCK:-}" ]]; then
    ready+=("ssh-agent detected (optional)")
  else
    ready+=("ssh-agent not detected; sidekick will fall back to ~/.ssh keys")
  fi

  echo
  for item in "${ready[@]}"; do
    echo "READY: ${item}"
  done

  if [[ "${#failures[@]}" -gt 0 ]]; then
    for item in "${failures[@]}"; do
      echo "ACTION REQUIRED: ${item}"
    done
    exit 1
  fi
}

main() {
  require_non_root_user
  require_repo_root
  prompt_server_ip

  install_system_packages
  install_go
  install_sops
  build_and_install_sidekick
  prepare_sidekick_config
  ensure_local_ssh_key
  ensure_known_host
  ensure_sidekick_user
  run_self_checks

  echo
  echo "Bootstrap complete. Next step: run 'sidekick init' as ${USER}."
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
