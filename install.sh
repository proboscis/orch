#!/usr/bin/env bash
#
# orch installer
# https://github.com/proboscis/orch
#
# Usage:
#   curl -sSL https://raw.githubusercontent.com/proboscis/orch/main/install.sh | bash
#   curl -sSL https://raw.githubusercontent.com/proboscis/orch/main/install.sh | bash -s -- --uninstall
#   curl -sSL https://raw.githubusercontent.com/proboscis/orch/main/install.sh | bash -s -- -y  # skip confirmation

set -euo pipefail

# Colors and formatting
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m' # No Color

INSTALLER_VERSION="1.0.0"
ORCH_REPO="proboscis/orch"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/orch"
TEMP_DIR=""

# Detected values
OS=""
ARCH=""
BINARY_URL=""
CHECKSUM_URL=""

# Flags
UNINSTALL=false
SKIP_CONFIRM=false
INSTALL_TUI=true
SETUP_COMPLETIONS=true
CREATE_CONFIG=true

# ============================================================================
# Utility functions
# ============================================================================

info() {
    printf "${BLUE}  %s${NC}\n" "$1"
}

success() {
    printf "${GREEN}  ✓ %s${NC}\n" "$1"
}

warn() {
    printf "${YELLOW}  ⚠ %s${NC}\n" "$1"
}

error() {
    printf "${RED}  ✗ %s${NC}\n" "$1" >&2
}

fatal() {
    error "$1"
    cleanup
    exit 1
}

cleanup() {
    if [[ -n "${TEMP_DIR:-}" && -d "${TEMP_DIR}" ]]; then
        rm -rf "${TEMP_DIR}"
    fi
}

trap cleanup EXIT

confirm() {
    if [[ "${SKIP_CONFIRM}" == "true" ]]; then
        return 0
    fi
    
    local prompt="${1:-Proceed?}"
    printf "\n  ${prompt} [Y/n] "
    read -r response
    case "${response}" in
        [nN][oO]|[nN])
            return 1
            ;;
        *)
            return 0
            ;;
    esac
}

command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# ============================================================================
# Detection functions
# ============================================================================

detect_os() {
    local uname_out
    uname_out="$(uname -s)"
    case "${uname_out}" in
        Linux*)     OS="linux";;
        Darwin*)    OS="darwin";;
        CYGWIN*|MINGW*|MSYS*)
            fatal "Windows is not supported. Please use WSL2."
            ;;
        *)
            fatal "Unsupported operating system: ${uname_out}"
            ;;
    esac
}

detect_arch() {
    local uname_m
    uname_m="$(uname -m)"
    case "${uname_m}" in
        x86_64|amd64)    ARCH="amd64";;
        arm64|aarch64)   ARCH="arm64";;
        *)
            fatal "Unsupported architecture: ${uname_m}"
            ;;
    esac
}

detect_shell() {
    local shell_name
    shell_name="$(basename "${SHELL:-/bin/bash}")"
    echo "${shell_name}"
}

get_shell_rc() {
    local shell_name="$1"
    case "${shell_name}" in
        bash)
            if [[ "${OS}" == "darwin" ]]; then
                echo "${HOME}/.bash_profile"
            else
                echo "${HOME}/.bashrc"
            fi
            ;;
        zsh)   echo "${HOME}/.zshrc";;
        fish)  echo "${HOME}/.config/fish/config.fish";;
        *)     echo "";;
    esac
}

# ============================================================================
# Dependency checking
# ============================================================================

check_dependency() {
    local cmd="$1"
    local name="${2:-$1}"
    local required="${3:-false}"
    
    if command_exists "${cmd}"; then
        local version
        case "${cmd}" in
            git)     version=$(git --version 2>/dev/null | awk '{print $3}');;
            go)      version=$(go version 2>/dev/null | awk '{print $3}' | sed 's/go//');;
            python3) version=$(python3 --version 2>/dev/null | awk '{print $2}');;
            python)  version=$(python --version 2>/dev/null | awk '{print $2}');;
            uv)      version=$(uv --version 2>/dev/null | awk '{print $2}');;
            tmux)    version=$(tmux -V 2>/dev/null | awk '{print $2}');;
            zellij)  version=$(zellij --version 2>/dev/null | awk '{print $2}');;
            *)       version="installed";;
        esac
        printf "    ${GREEN}✓${NC} %s %s\n" "${name}" "${version}"
        return 0
    else
        if [[ "${required}" == "true" ]]; then
            printf "    ${RED}✗${NC} %s ${RED}(required)${NC}\n" "${name}"
        else
            printf "    ${YELLOW}○${NC} %s ${YELLOW}(optional)${NC}\n" "${name}"
        fi
        return 1
    fi
}

check_python() {
    # Check for Python 3.10+
    local python_cmd=""
    local python_version=""
    
    if command_exists python3; then
        python_cmd="python3"
    elif command_exists python; then
        python_cmd="python"
    fi
    
    if [[ -n "${python_cmd}" ]]; then
        python_version=$("${python_cmd}" -c 'import sys; print(f"{sys.version_info.major}.{sys.version_info.minor}")' 2>/dev/null || echo "")
        if [[ -n "${python_version}" ]]; then
            local major minor
            major=$(echo "${python_version}" | cut -d. -f1)
            minor=$(echo "${python_version}" | cut -d. -f2)
            if [[ "${major}" -ge 3 && "${minor}" -ge 10 ]]; then
                printf "    ${GREEN}✓${NC} python %s\n" "${python_version}"
                return 0
            else
                printf "    ${YELLOW}○${NC} python %s ${YELLOW}(3.10+ required for TUI)${NC}\n" "${python_version}"
                return 1
            fi
        fi
    fi
    
    printf "    ${YELLOW}○${NC} python ${YELLOW}(3.10+ required for TUI)${NC}\n"
    return 1
}

check_multiplexer() {
    if command_exists tmux; then
        local version=$(tmux -V 2>/dev/null | awk '{print $2}')
        printf "    ${GREEN}✓${NC} tmux %s\n" "${version}"
        return 0
    elif command_exists zellij; then
        local version=$(zellij --version 2>/dev/null | awk '{print $2}')
        printf "    ${GREEN}✓${NC} zellij %s\n" "${version}"
        return 0
    else
        printf "    ${RED}✗${NC} tmux or zellij ${RED}(required)${NC}\n"
        return 1
    fi
}

# ============================================================================
# Installation functions
# ============================================================================

check_github_releases() {
    # Check if pre-built binaries exist
    local release_url="https://api.github.com/repos/${ORCH_REPO}/releases/latest"
    local release_info
    
    if command_exists curl; then
        release_info=$(curl -sS "${release_url}" 2>/dev/null || echo "")
    elif command_exists wget; then
        release_info=$(wget -qO- "${release_url}" 2>/dev/null || echo "")
    fi
    
    if [[ -z "${release_info}" || "${release_info}" == *"Not Found"* ]]; then
        return 1
    fi
    
    # Look for binary matching our OS/ARCH
    local binary_name="orch-${OS}-${ARCH}"
    if echo "${release_info}" | grep -q "\"name\": \"${binary_name}\""; then
        BINARY_URL=$(echo "${release_info}" | grep -o "\"browser_download_url\": \"[^\"]*${binary_name}\"" | head -1 | cut -d'"' -f4)
        CHECKSUM_URL=$(echo "${release_info}" | grep -o "\"browser_download_url\": \"[^\"]*checksums.txt\"" | head -1 | cut -d'"' -f4)
        return 0
    fi
    
    return 1
}

download_binary() {
    info "Downloading orch binary..."
    
    TEMP_DIR=$(mktemp -d)
    local binary_path="${TEMP_DIR}/orch"
    
    if command_exists curl; then
        curl -sSL "${BINARY_URL}" -o "${binary_path}" || fatal "Failed to download binary"
    elif command_exists wget; then
        wget -q "${BINARY_URL}" -O "${binary_path}" || fatal "Failed to download binary"
    else
        fatal "Neither curl nor wget found"
    fi
    
    # Verify checksum if available
    if [[ -n "${CHECKSUM_URL}" ]]; then
        local checksums_path="${TEMP_DIR}/checksums.txt"
        if command_exists curl; then
            curl -sSL "${CHECKSUM_URL}" -o "${checksums_path}" 2>/dev/null
        elif command_exists wget; then
            wget -q "${CHECKSUM_URL}" -O "${checksums_path}" 2>/dev/null
        fi
        
        if [[ -f "${checksums_path}" ]]; then
            local expected_checksum
            expected_checksum=$(grep "orch-${OS}-${ARCH}" "${checksums_path}" | awk '{print $1}')
            if [[ -n "${expected_checksum}" ]]; then
                local actual_checksum
                if command_exists sha256sum; then
                    actual_checksum=$(sha256sum "${binary_path}" | awk '{print $1}')
                elif command_exists shasum; then
                    actual_checksum=$(shasum -a 256 "${binary_path}" | awk '{print $1}')
                fi
                
                if [[ -n "${actual_checksum}" && "${expected_checksum}" != "${actual_checksum}" ]]; then
                    fatal "Checksum verification failed"
                fi
                success "Checksum verified"
            fi
        fi
    fi
    
    chmod +x "${binary_path}"
    
    # macOS code signing
    if [[ "${OS}" == "darwin" ]]; then
        codesign --force --sign - "${binary_path}" 2>/dev/null || true
    fi
    
    mkdir -p "${INSTALL_DIR}"
    mv "${binary_path}" "${INSTALL_DIR}/orch"
    success "Downloaded orch-${OS}-${ARCH}"
}

build_from_source() {
    info "Building orch from source..."
    
    if ! command_exists go; then
        fatal "Go is required to build from source. Install Go from https://go.dev/dl/"
    fi
    
    if ! command_exists git; then
        fatal "git is required to build from source"
    fi
    
    TEMP_DIR=$(mktemp -d)
    
    info "Cloning repository..."
    git clone --depth 1 "https://github.com/${ORCH_REPO}.git" "${TEMP_DIR}/orch" 2>/dev/null || fatal "Failed to clone repository"
    
    info "Building binary..."
    (cd "${TEMP_DIR}/orch" && go build -o orch ./cmd/orch) || fatal "Failed to build orch"
    
    mkdir -p "${INSTALL_DIR}"
    mv "${TEMP_DIR}/orch/orch" "${INSTALL_DIR}/orch"
    
    # macOS code signing
    if [[ "${OS}" == "darwin" ]]; then
        codesign --force --sign - "${INSTALL_DIR}/orch" 2>/dev/null || true
    fi
    
    success "Built and installed orch"
}

install_orch_monitor() {
    info "Installing orch-monitor TUI..."
    
    if ! command_exists uv; then
        warn "uv not found, attempting to install..."
        if command_exists curl; then
            curl -LsSf https://astral.sh/uv/install.sh | sh 2>/dev/null || {
                warn "Failed to install uv. Skipping TUI installation."
                return 1
            }
            # Source the updated PATH
            export PATH="${HOME}/.local/bin:${HOME}/.cargo/bin:${PATH}"
        else
            warn "curl not found. Skipping TUI installation."
            return 1
        fi
    fi
    
    # Clone or use existing source
    if [[ -d "${TEMP_DIR}/orch/orch-monitor-tui" ]]; then
        (cd "${TEMP_DIR}/orch" && uv tool install --force ./orch-monitor-tui 2>/dev/null) || {
            warn "Failed to install orch-monitor TUI"
            return 1
        }
    else
        # Need to clone the repo
        local clone_dir="${TEMP_DIR:-$(mktemp -d)}/orch-clone"
        if [[ ! -d "${clone_dir}" ]]; then
            git clone --depth 1 "https://github.com/${ORCH_REPO}.git" "${clone_dir}" 2>/dev/null || {
                warn "Failed to clone repository for TUI installation"
                return 1
            }
        fi
        (cd "${clone_dir}" && uv tool install --force ./orch-monitor-tui 2>/dev/null) || {
            warn "Failed to install orch-monitor TUI"
            return 1
        }
    fi
    
    success "Installed orch-monitor TUI"
    return 0
}

setup_shell_completions() {
    local shell_name
    shell_name=$(detect_shell)
    
    if [[ -z "${shell_name}" ]]; then
        warn "Could not detect shell, skipping completions"
        return 1
    fi
    
    info "Setting up ${shell_name} completions..."
    
    local rc_file
    rc_file=$(get_shell_rc "${shell_name}")
    
    if [[ -z "${rc_file}" ]]; then
        warn "Unknown shell: ${shell_name}, skipping completions"
        return 1
    fi
    
    local completion_cmd=""
    case "${shell_name}" in
        bash)
            completion_cmd='eval "$(orch completion bash)"'
            ;;
        zsh)
            completion_cmd='eval "$(orch completion zsh)"'
            ;;
        fish)
            completion_cmd='orch completion fish | source'
            ;;
        *)
            warn "Completions not supported for ${shell_name}"
            return 1
            ;;
    esac
    
    # Check if already configured
    if [[ -f "${rc_file}" ]] && grep -q "orch completion" "${rc_file}"; then
        success "Completions already configured in ${rc_file}"
        return 0
    fi
    
    # Add completion command
    {
        echo ""
        echo "# orch shell completions"
        echo "${completion_cmd}"
    } >> "${rc_file}"
    
    success "Added completions to ${rc_file}"
}

create_default_config() {
    if [[ -f "${CONFIG_DIR}/config.yaml" ]]; then
        success "Config already exists at ${CONFIG_DIR}/config.yaml"
        return 0
    fi
    
    info "Creating default config..."
    mkdir -p "${CONFIG_DIR}"
    
    cat > "${CONFIG_DIR}/config.yaml" << 'CONFIGEOF'
# orch configuration
# See: https://github.com/proboscis/orch

# Default CLI agent (claude, codex, opencode, gemini)
# agent: claude

# Terminal multiplexer (tmux, zellij)
# multiplexer: tmux

# Issues directory (local file backend)
# issues_root: ~/orch-issues

# Worktrees directory for run isolation
# worktrees_root: ~/.orch/worktrees
CONFIGEOF
    
    success "Created config at ${CONFIG_DIR}/config.yaml"
}

update_path() {
    if [[ ":${PATH}:" == *":${INSTALL_DIR}:"* ]]; then
        return 0
    fi
    
    local shell_name
    shell_name=$(detect_shell)
    local rc_file
    rc_file=$(get_shell_rc "${shell_name}")
    
    if [[ -z "${rc_file}" ]]; then
        warn "Add ${INSTALL_DIR} to your PATH manually"
        return 1
    fi
    
    # Check if already configured
    if [[ -f "${rc_file}" ]] && grep -q "${INSTALL_DIR}" "${rc_file}"; then
        return 0
    fi
    
    info "Adding ${INSTALL_DIR} to PATH..."
    
    {
        echo ""
        echo "# orch - add to PATH"
        echo "export PATH=\"${INSTALL_DIR}:\${PATH}\""
    } >> "${rc_file}"
    
    success "Added ${INSTALL_DIR} to PATH in ${rc_file}"
    warn "Run 'source ${rc_file}' or start a new shell to update PATH"
}

# ============================================================================
# Uninstall
# ============================================================================

do_uninstall() {
    echo ""
    printf "  ${BOLD}orch uninstaller${NC}\n"
    echo ""
    
    local items_to_remove=()
    
    if [[ -f "${INSTALL_DIR}/orch" ]]; then
        items_to_remove+=("${INSTALL_DIR}/orch")
    fi
    
    # Check for uv-installed orch-monitor
    local tui_path
    if command_exists uv; then
        tui_path=$(uv tool dir 2>/dev/null)/orch-monitor 2>/dev/null || true
        if [[ -n "${tui_path}" && -d "${tui_path}" ]]; then
            items_to_remove+=("orch-monitor (uv tool)")
        fi
    fi
    
    if [[ -d "${CONFIG_DIR}" ]]; then
        items_to_remove+=("${CONFIG_DIR}")
    fi
    
    if [[ ${#items_to_remove[@]} -eq 0 ]]; then
        info "Nothing to uninstall"
        return 0
    fi
    
    echo "  Will remove:"
    for item in "${items_to_remove[@]}"; do
        echo "    - ${item}"
    done
    
    if ! confirm "Proceed with uninstall?"; then
        info "Uninstall cancelled"
        return 0
    fi
    
    # Remove orch binary
    if [[ -f "${INSTALL_DIR}/orch" ]]; then
        rm -f "${INSTALL_DIR}/orch"
        success "Removed ${INSTALL_DIR}/orch"
    fi
    
    # Uninstall orch-monitor via uv
    if command_exists uv; then
        uv tool uninstall orch-monitor 2>/dev/null && success "Removed orch-monitor" || true
    fi
    
    # Remove config (ask separately)
    if [[ -d "${CONFIG_DIR}" ]]; then
        if confirm "Also remove configuration at ${CONFIG_DIR}?"; then
            rm -rf "${CONFIG_DIR}"
            success "Removed ${CONFIG_DIR}"
        fi
    fi
    
    echo ""
    success "orch has been uninstalled"
    warn "Shell completions in your rc file need to be removed manually"
}

# ============================================================================
# Main installation flow
# ============================================================================

parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --uninstall)
                UNINSTALL=true
                shift
                ;;
            -y|--yes)
                SKIP_CONFIRM=true
                shift
                ;;
            --no-tui)
                INSTALL_TUI=false
                shift
                ;;
            --no-completions)
                SETUP_COMPLETIONS=false
                shift
                ;;
            --no-config)
                CREATE_CONFIG=false
                shift
                ;;
            --install-dir)
                INSTALL_DIR="$2"
                shift 2
                ;;
            -h|--help)
                show_help
                exit 0
                ;;
            *)
                fatal "Unknown option: $1"
                ;;
        esac
    done
}

show_help() {
    cat << EOF
orch installer v${INSTALLER_VERSION}

Usage:
  curl -sSL https://raw.githubusercontent.com/${ORCH_REPO}/main/install.sh | bash
  curl -sSL ... | bash -s -- [options]

Options:
  --uninstall       Uninstall orch
  -y, --yes         Skip confirmation prompts
  --no-tui          Skip orch-monitor TUI installation
  --no-completions  Skip shell completions setup
  --no-config       Skip default config creation
  --install-dir DIR Install to specified directory (default: ~/.local/bin)
  -h, --help        Show this help message

Examples:
  # Standard installation
  curl -sSL https://raw.githubusercontent.com/${ORCH_REPO}/main/install.sh | bash

  # Non-interactive installation
  curl -sSL ... | bash -s -- -y

  # Uninstall
  curl -sSL ... | bash -s -- --uninstall
EOF
}

do_install() {
    echo ""
    printf "  ${BOLD}orch installer v${INSTALLER_VERSION}${NC}\n"
    echo ""
    
    # Detect system
    detect_os
    detect_arch
    printf "  Detected: ${BOLD}${OS} ${ARCH}${NC}\n"
    echo ""
    
    # Check dependencies
    echo "  Dependencies:"
    local deps_ok=true
    local has_go=false
    local has_python=false
    local has_multiplexer=false
    
    check_dependency git git true || deps_ok=false
    check_dependency go go false && has_go=true
    check_python && has_python=true
    check_multiplexer && has_multiplexer=true
    check_dependency uv uv false
    
    echo ""
    
    if [[ "${has_multiplexer}" != "true" ]]; then
        fatal "tmux or zellij is required. Install one of them first."
    fi
    
    # Determine installation method
    local install_method="source"
    if check_github_releases 2>/dev/null; then
        install_method="binary"
    fi
    
    # Show what will be installed
    echo "  Will install:"
    if [[ "${install_method}" == "binary" ]]; then
        echo "    - orch CLI (pre-built binary) -> ${INSTALL_DIR}/orch"
    else
        if [[ "${has_go}" != "true" ]]; then
            fatal "Go is required to build from source (no pre-built binaries available). Install Go from https://go.dev/dl/"
        fi
        echo "    - orch CLI (build from source) -> ${INSTALL_DIR}/orch"
    fi
    
    if [[ "${INSTALL_TUI}" == "true" && "${has_python}" == "true" ]]; then
        echo "    - orch-monitor TUI (via uv)"
    fi
    
    if [[ "${SETUP_COMPLETIONS}" == "true" ]]; then
        local shell_name
        shell_name=$(detect_shell)
        echo "    - shell completions for ${shell_name}"
    fi
    
    if [[ "${CREATE_CONFIG}" == "true" ]]; then
        echo "    - default config at ${CONFIG_DIR}/config.yaml"
    fi
    
    echo ""
    
    if ! confirm "Proceed?"; then
        info "Installation cancelled"
        exit 0
    fi
    
    echo ""
    
    # Install orch CLI
    if [[ "${install_method}" == "binary" ]]; then
        download_binary
    else
        build_from_source
    fi
    
    # Update PATH
    update_path
    
    # Install TUI
    if [[ "${INSTALL_TUI}" == "true" && "${has_python}" == "true" ]]; then
        install_orch_monitor || true
    fi
    
    # Setup completions
    if [[ "${SETUP_COMPLETIONS}" == "true" ]]; then
        setup_shell_completions || true
    fi
    
    # Create default config
    if [[ "${CREATE_CONFIG}" == "true" ]]; then
        create_default_config || true
    fi
    
    echo ""
    success "Done! Run 'orch --help' to get started."
    
    # Remind about PATH if needed
    if [[ ":${PATH}:" != *":${INSTALL_DIR}:"* ]]; then
        echo ""
        warn "Don't forget to restart your shell or run:"
        echo "    export PATH=\"${INSTALL_DIR}:\${PATH}\""
    fi
}

main() {
    parse_args "$@"
    
    if [[ "${UNINSTALL}" == "true" ]]; then
        do_uninstall
    else
        do_install
    fi
}

main "$@"
