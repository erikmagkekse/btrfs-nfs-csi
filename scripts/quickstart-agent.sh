#!/usr/bin/env bash
# quickstart-agent.sh - quick start installer for the btrfs-nfs-csi agent (Podman Quadlet)
#
# Usage:
#   Install:    curl -fsSL https://raw.githubusercontent.com/erikmagkekse/btrfs-nfs-csi/main/scripts/quickstart-agent.sh | sudo -E bash
#   Update:     curl -fsSL https://raw.githubusercontent.com/erikmagkekse/btrfs-nfs-csi/main/scripts/quickstart-agent.sh | sudo -E bash
#               (detects existing install, preserves config, updates image + quadlet)
#   Uninstall:  curl -fsSL https://raw.githubusercontent.com/erikmagkekse/btrfs-nfs-csi/main/scripts/quickstart-agent.sh | sudo -E bash -s -- --uninstall
#
# Flags:
#   --yes, -y             skip confirmation prompts (e.g. for CI/automation)
#   --uninstall           remove agent (config + quadlet, keeps data)
#
# Environment variables:
#   AGENT_BASE_PATH       btrfs mount point            (default: /export/data)
#   AGENT_TENANTS         tenant:token pairs            (default: default:<random>)
#   AGENT_LISTEN_ADDR     listen address                (default: :8080)
#   VERSION               image tag                     (default: 0.9.6)
#   AGENT_BLOCK_DISK      block device to auto-format as btrfs and mount (e.g. /dev/sdb, install-only, uses mkfs.btrfs -f!)
#   SKIP_PACKAGE_INSTALL  set to 1 to skip apt/dnf/pacman

set -euo pipefail

# flags
YES=false
UNINSTALL=false
for arg in "$@"; do
    case "${arg}" in
        --yes|-y) YES=true ;;
        --uninstall) UNINSTALL=true ;;
    esac
done

# defaults
AGENT_BASE_PATH="${AGENT_BASE_PATH:-/export/data}"
AGENT_LISTEN_ADDR="${AGENT_LISTEN_ADDR:-:8080}"
VERSION="${VERSION:-0.9.6}"
AGENT_BLOCK_DISK="${AGENT_BLOCK_DISK:-}"
SKIP_PACKAGE_INSTALL="${SKIP_PACKAGE_INSTALL:-}"

REPO_RAW="https://raw.githubusercontent.com/erikmagkekse/btrfs-nfs-csi/main"
IMAGE="ghcr.io/erikmagkekse/btrfs-nfs-csi:${VERSION}"
CONFIG_DIR="/etc/btrfs-nfs-csi"
QUADLET_DIR="/etc/containers/systemd"
QUADLET_FILE="${QUADLET_DIR}/btrfs-nfs-csi-agent.container"
SERVICE_NAME="btrfs-nfs-csi-agent"

# helpers
info()  { printf '\033[1;34m[INFO]\033[0m  %s\n' "$*"; }
warn()  { printf '\033[1;33m[WARN]\033[0m  %s\n' "$*"; }
error() { printf '\033[1;31m[ERROR]\033[0m %s\n' "$*" >&2; }
fatal() { error "$@"; exit 1; }

confirm() {
    if ${YES}; then return 0; fi
    printf '\033[1;33m[?]\033[0m     %s [y/N] ' "$1"
    read -r answer </dev/tty
    [[ "${answer}" =~ ^[Yy]$ ]]
}

detect_distro() {
    if [[ -f /etc/os-release ]]; then
        # shellcheck source=/dev/null
        . /etc/os-release
        case "${ID:-}${ID_LIKE:-}" in
            *debian*|*ubuntu*) echo "debian" ;;
            *rhel*|*fedora*|*centos*) echo "rhel" ;;
            *arch*) echo "arch" ;;
            *suse*|*opensuse*) echo "suse" ;;
            *) echo "unknown" ;;
        esac
    else
        echo "unknown"
    fi
}

generate_token() {
    if command -v openssl &>/dev/null; then
        openssl rand -hex 16
    else
        head -c 16 /dev/urandom | od -A n -t x1 | tr -d ' \n'
    fi
}

# uninstall
if ${UNINSTALL}; then
    [[ $EUID -eq 0 ]] || fatal "This script must be run as root."
    confirm "Uninstall btrfs-nfs-csi agent? Data on ${AGENT_BASE_PATH} will be kept." || { info "Aborted."; exit 0; }
    info "Uninstalling btrfs-nfs-csi agent..."

    if systemctl is-active --quiet "${SERVICE_NAME}" 2>/dev/null; then
        info "Stopping ${SERVICE_NAME}..."
        systemctl disable --now "${SERVICE_NAME}"
    fi

    if [[ -f "${QUADLET_FILE}" ]]; then
        info "Removing Quadlet file..."
        rm -f "${QUADLET_FILE}"
        systemctl daemon-reload
    fi

    if [[ -d "${CONFIG_DIR}" ]]; then
        info "Removing config directory ${CONFIG_DIR}..."
        rm -rf "${CONFIG_DIR}"
    fi

    info "Uninstall complete. Data on ${AGENT_BASE_PATH} was NOT removed."
    exit 0
fi

# install / upgrade
UPGRADE=false
[[ -f "${CONFIG_DIR}/agent.env" ]] && UPGRADE=true

[[ $EUID -eq 0 ]] || fatal "This script must be run as root."

if ${UPGRADE}; then
    info "Existing installation detected at ${CONFIG_DIR}/agent.env"
    confirm "Upgrade/reinstall agent to v${VERSION}?" || { info "Aborted."; exit 0; }
    info "btrfs-nfs-csi agent upgrade/reinstall to v${VERSION}"
else
    info "btrfs-nfs-csi agent install v${VERSION}"
fi

# 1. packages
DISTRO=$(detect_distro)
info "Detected distro family: ${DISTRO}"

if [[ "${SKIP_PACKAGE_INSTALL}" == "1" ]]; then
    info "Skipping package installation (SKIP_PACKAGE_INSTALL=1)."
else
    info "Installing prerequisites..."
    case "${DISTRO}" in
        debian)
            export DEBIAN_FRONTEND=noninteractive
            apt-get update -qq
            apt-get install -y -qq podman nfs-kernel-server btrfs-progs
            ;;
        rhel)
            dnf install -y podman nfs-utils btrfs-progs
            ;;
        arch)
            pacman -Sy --noconfirm --needed podman nfs-utils btrfs-progs
            ;;
        suse)
            zypper install -y podman nfs-kernel-server btrfsprogs
            ;;
        *)
            warn "Unknown distro - please install podman, nfs-utils, and btrfs-progs manually."
            warn "Then re-run with SKIP_PACKAGE_INSTALL=1."
            fatal "Cannot auto-install packages for distro: ${DISTRO}"
            ;;
    esac
    info "Packages installed."
fi

# 2. block device (fresh install only)
if [[ -n "${AGENT_BLOCK_DISK}" ]] && ! ${UPGRADE}; then
    [[ -b "${AGENT_BLOCK_DISK}" ]] || fatal "${AGENT_BLOCK_DISK} is not a block device."

    if findmnt -n --source "${AGENT_BLOCK_DISK}" &>/dev/null; then
        existing_mount=$(findmnt -n -o TARGET --source "${AGENT_BLOCK_DISK}")
        fatal "${AGENT_BLOCK_DISK} is already mounted at ${existing_mount}. Unmount it first or remove AGENT_BLOCK_DISK."
    fi

    info "Formatting ${AGENT_BLOCK_DISK} as btrfs..."
    mkfs.btrfs -f "${AGENT_BLOCK_DISK}"

    mkdir -p "${AGENT_BASE_PATH}"
    info "Mounting ${AGENT_BLOCK_DISK} at ${AGENT_BASE_PATH}..."
    mount "${AGENT_BLOCK_DISK}" "${AGENT_BASE_PATH}"

    disk_uuid=$(blkid -s UUID -o value "${AGENT_BLOCK_DISK}")
    if ! grep -q "${disk_uuid}" /etc/fstab; then
        echo "UUID=${disk_uuid}  ${AGENT_BASE_PATH}  btrfs  defaults  0  0" >> /etc/fstab
        info "Added fstab entry for UUID=${disk_uuid}."
    fi

    info "Enabling btrfs quotas..."
    btrfs quota enable "${AGENT_BASE_PATH}"
fi

# 3. verify btrfs
mountpoint -q "${AGENT_BASE_PATH}" 2>/dev/null || fatal "${AGENT_BASE_PATH} is not a mount point. Mount a btrfs filesystem there first."

fstype=$(findmnt -n -o FSTYPE --target "${AGENT_BASE_PATH}")
[[ "${fstype}" == "btrfs" ]] || fatal "${AGENT_BASE_PATH} is ${fstype}, not btrfs."

if ! btrfs qgroup show "${AGENT_BASE_PATH}" &>/dev/null; then
    warn "btrfs quotas not enabled on ${AGENT_BASE_PATH}, enabling now..."
    btrfs quota enable "${AGENT_BASE_PATH}"
    info "Quotas enabled."
fi

# 4. NFS server
info "Enabling nfs-server..."
systemctl enable --now nfs-server

# 5. tenant config
if ${UPGRADE} && [[ -z "${AGENT_TENANTS:-}" ]]; then
    AGENT_TENANTS=$(grep -oP '^AGENT_TENANTS=\K.*' "${CONFIG_DIR}/agent.env" || true)
    if [[ -n "${AGENT_TENANTS}" ]]; then
        info "Preserving existing tenant config from ${CONFIG_DIR}/agent.env"
    fi
fi

if [[ -z "${AGENT_TENANTS:-}" ]]; then
    TOKEN=$(generate_token)
    AGENT_TENANTS="default:${TOKEN}"
    info "Generated tenant token (save this!):"
    echo ""
    echo "    AGENT_TENANTS=${AGENT_TENANTS}"
    echo ""
fi

# 6. write config
install -d -m 700 "${CONFIG_DIR}"

cat > "${CONFIG_DIR}/agent.env" <<EOF
AGENT_BASE_PATH=${AGENT_BASE_PATH}
AGENT_TENANTS=${AGENT_TENANTS}
AGENT_LISTEN_ADDR=${AGENT_LISTEN_ADDR}
EOF
chmod 600 "${CONFIG_DIR}/agent.env"

if ${UPGRADE}; then
    info "Config updated at ${CONFIG_DIR}/agent.env"
else
    info "Config written to ${CONFIG_DIR}/agent.env"
fi

# 7. quadlet
install -d -m 755 "${QUADLET_DIR}"

info "Downloading Quadlet unit file..."
curl -fsSL "${REPO_RAW}/deploy/agent/btrfs-nfs-csi-agent.container" -o "${QUADLET_FILE}"

sed -i "s|^Image=.*|Image=${IMAGE}|" "${QUADLET_FILE}"
sed -i "s|^Volume=/export/data:/export/data|Volume=${AGENT_BASE_PATH}:${AGENT_BASE_PATH}|" "${QUADLET_FILE}"

info "Quadlet file installed to ${QUADLET_FILE}"

# 8. pull + start / restart
info "Pulling image ${IMAGE}..."
podman pull "${IMAGE}"

systemctl daemon-reload
if ${UPGRADE}; then
    info "Restarting ${SERVICE_NAME}..."
    systemctl restart "${SERVICE_NAME}"
else
    info "Starting ${SERVICE_NAME}..."
    systemctl start "${SERVICE_NAME}"
fi

# 9. health check
LISTEN_PORT="${AGENT_LISTEN_ADDR##*:}"
LISTEN_PORT="${LISTEN_PORT:-8080}"
HEALTHZ_URL="http://localhost:${LISTEN_PORT}/healthz"

info "Waiting for agent to become healthy..."
healthy=false
for _ in $(seq 1 10); do
    if curl -sf "${HEALTHZ_URL}" &>/dev/null; then
        healthy=true
        break
    fi
    sleep 1
done

echo ""
if ${healthy}; then
    info "Agent is healthy!"
else
    warn "Agent not yet responding on ${HEALTHZ_URL}."
    warn "Check logs: journalctl -u ${SERVICE_NAME} -f"
fi

# 10. summary
if ${UPGRADE}; then ACTION="upgraded"; else ACTION="installed"; fi

cat <<EOF

  btrfs-nfs-csi agent ${ACTION} successfully!

  Config:     ${CONFIG_DIR}/agent.env
  Quadlet:    ${QUADLET_FILE}
  Service:    ${SERVICE_NAME}
  Health:     ${HEALTHZ_URL}
  Base path:  ${AGENT_BASE_PATH}

  Tenant config:
    ${AGENT_TENANTS}

EOF

if ! ${UPGRADE}; then
    cat <<EOF
  Next steps:
    1. Save the tenant token above - you'll need it for the
       Kubernetes StorageClass secret.
    2. Deploy the CSI driver in your cluster:
       kubectl apply -f ${REPO_RAW}/deploy/driver/setup.yaml
    3. See full docs: https://github.com/erikmagkekse/btrfs-nfs-csi/blob/main/docs/installation.md

EOF
fi
