#!/bin/bash
# Installs Sidekick and local prerequisites on Ubuntu.
# Run this as a regular user with sudo privileges, not as root.

set -euo pipefail

if [ "${EUID}" -eq 0 ]; then
  echo "Error: do not run this script as root."
  echo "Log in as a regular user (for example: admin) and try again."
  exit 1
fi

echo "=========================================="
echo "Starting Sidekick installation (Ubuntu)"
echo "=========================================="

read -r -p "Enter this server's public IPv4 address: " SERVER_IP

echo "Installing system packages..."
sudo apt-get update
sudo apt-get install -y age ca-certificates curl git openssh-client snapd

echo "Removing old apt-based Go packages if present..."
sudo apt-get remove -y golang golang-go golang-src golang-doc || true
sudo apt-get autoremove -y

echo "Installing Go from Snap..."
sudo systemctl enable --now snapd.socket
if snap list go >/dev/null 2>&1; then
  sudo snap refresh go --classic
else
  sudo snap install go --classic
fi

export PATH="${PATH}:/snap/bin"
echo "Go version: $(GOTOOLCHAIN=local go version)"

echo "Installing SOPS if needed..."
if ! command -v sops >/dev/null 2>&1; then
  SOPS_VERSION="v3.9.0"
  curl -sLO "https://github.com/getsops/sops/releases/download/${SOPS_VERSION}/sops-${SOPS_VERSION}.linux.amd64"
  sudo mv "sops-${SOPS_VERSION}.linux.amd64" /usr/local/bin/sops
  sudo chmod +x /usr/local/bin/sops
  echo "SOPS installed."
else
  echo "SOPS is already installed."
fi

echo "Installing Docker if needed..."
if ! command -v docker >/dev/null 2>&1; then
  sudo apt-get install -y docker.io
  sudo systemctl enable --now docker
else
  echo "Docker is already installed."
fi
sudo usermod -aG docker "${USER}"

echo "Verifying dependencies..."
which age >/dev/null || { echo "Error: age not found in PATH"; exit 1; }
which sops >/dev/null || { echo "Error: sops not found in PATH"; exit 1; }
which docker >/dev/null || { echo "Error: docker not found in PATH"; exit 1; }

echo "Building Sidekick from source..."
rm -rf "${HOME}/sidekick-repo"
git clone https://github.com/filipvlcekk/sidekick.git "${HOME}/sidekick-repo"
cd "${HOME}/sidekick-repo"

GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}" \
GOTOOLCHAIN=local \
go build -o sidekick .

echo "Installing Sidekick to /usr/local/bin..."
sudo mv sidekick /usr/local/bin/
sudo chmod +x /usr/local/bin/sidekick
cd "${HOME}"
rm -rf "${HOME}/sidekick-repo"

echo "Preparing Sidekick config directories..."
mkdir -p "${HOME}/.config/sidekick"
sudo mkdir -p /root/.config/sidekick

echo "Sidekick version:"
sidekick version || echo "Version check skipped."

echo "Setting up SSH keys for ${USER}..."
mkdir -p "${HOME}/.ssh"
chmod 700 "${HOME}/.ssh"

if [ ! -f "${HOME}/.ssh/id_ed25519" ]; then
  ssh-keygen -t ed25519 -N "" -f "${HOME}/.ssh/id_ed25519"
fi

cat "${HOME}/.ssh/id_ed25519.pub" >> "${HOME}/.ssh/authorized_keys"
chmod 600 "${HOME}/.ssh/authorized_keys"

ssh-keyscan "${SERVER_IP}" >> "${HOME}/.ssh/known_hosts" 2>/dev/null
chmod 644 "${HOME}/.ssh/known_hosts"

echo "Copying SSH access to root for the initial bootstrap..."
sudo mkdir -p /root/.ssh
sudo chmod 700 /root/.ssh
sudo cp "${HOME}/.ssh/authorized_keys" /root/.ssh/authorized_keys
sudo chown root:root /root/.ssh/authorized_keys
sudo chmod 600 /root/.ssh/authorized_keys
sudo sh -c "ssh-keyscan ${SERVER_IP} >> /root/.ssh/known_hosts 2>/dev/null"

echo "=========================================="
echo "Installation complete"
echo "=========================================="
echo
echo "Notes:"
echo "- Docker group membership was updated for ${USER}."
echo "- Open a new shell or log out and back in before using Docker without sudo."
echo
echo "Recommended next steps:"
echo '  eval "$(ssh-agent -s)"'
echo "  ssh-add ~/.ssh/id_ed25519"
echo "  sudo -E sidekick init"
