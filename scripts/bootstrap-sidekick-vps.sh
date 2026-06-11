#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_PLUGIN_VERSION="v2.39.2"

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

docker_apt_repo_line() {
  local arch codename
  arch="${DOCKER_APT_ARCH:-}"
  codename="${DOCKER_APT_CODENAME:-}"

  if [[ -z "${arch}" ]]; then
    arch="$(dpkg --print-architecture)"
  fi
  if [[ -z "${codename}" ]]; then
    codename="$(
      . /etc/os-release
      echo "$VERSION_CODENAME"
    )"
  fi

  printf 'deb [arch=%s signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu %s stable' \
    "${arch}" \
    "${codename}"
}

compose_plugin_install_path() {
  printf '/usr/local/lib/docker/cli-plugins/docker-compose'
}

compose_plugin_download_url() {
  local arch
  arch="${COMPOSE_PLUGIN_ARCH:-$(uname -m)}"

  case "${arch}" in
    x86_64)
      printf 'https://github.com/docker/compose/releases/download/%s/docker-compose-linux-x86_64' "${COMPOSE_PLUGIN_VERSION}"
      ;;
    aarch64|arm64)
      printf 'https://github.com/docker/compose/releases/download/%s/docker-compose-linux-aarch64' "${COMPOSE_PLUGIN_VERSION}"
      ;;
    *)
      echo "Error: unsupported architecture for docker compose plugin fallback: ${arch}" >&2
      return 1
      ;;
  esac
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
  sudo apt-get install -y age ca-certificates curl git openssh-client snapd
}

install_docker_from_repo() {
  sudo install -m 0755 -d /etc/apt/keyrings
  sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
  sudo chmod a+r /etc/apt/keyrings/docker.asc
  docker_apt_repo_line | sudo tee /etc/apt/sources.list.d/docker.list >/dev/null
  sudo apt-get update
  sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
}

install_compose_plugin_fallback() {
  log_section "Installing docker compose plugin fallback"

  local plugin_dir plugin_path plugin_url tmp_file
  plugin_dir="$(dirname "$(compose_plugin_install_path)")"
  plugin_path="$(compose_plugin_install_path)"
  plugin_url="$(compose_plugin_download_url)"
  tmp_file="$(mktemp)"

  sudo mkdir -p "${plugin_dir}"
  curl -fsSL "${plugin_url}" -o "${tmp_file}"
  sudo mv "${tmp_file}" "${plugin_path}"
  sudo chmod +x "${plugin_path}"
}

install_docker() {
  log_section "Installing Docker"
  if install_docker_from_repo; then
    :
  else
    echo "Official Docker apt install failed; falling back to distro Docker plus direct compose plugin install."
    sudo rm -f /etc/apt/sources.list.d/docker.list
    sudo apt-get update
    sudo apt-get install -y docker.io
    install_compose_plugin_fallback
  fi

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
  sudo usermod -aG docker sidekick || true
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

  if docker compose version >/dev/null 2>&1; then
    ready+=("docker compose available for ${USER}")
  else
    failures+=("docker compose is not available for ${USER}")
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

  if sudo -u sidekick -H bash -lc 'docker compose version' >/dev/null 2>&1; then
    ready+=("docker compose available for sidekick user")
  else
    failures+=("docker compose is not available for sidekick user")
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
  install_docker
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
