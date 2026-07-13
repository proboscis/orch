#!/bin/bash
# orch installer - https://github.com/proboscis/orch
# curl -sSL https://raw.githubusercontent.com/proboscis/orch/main/install.sh | bash
set -euo pipefail

VERSION="1.1.0"
REPO="proboscis/orch"
INSTALL_DIR="${HOME}/.local/bin"
CONFIG_DIR="${HOME}/.config/orch"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m'

# Mode flags
AUTO_MODE=false
UNINSTALL=false

# Component flags (for auto mode)
INSTALL_OPENCODE=false
INSTALL_CLAUDE=false
INSTALL_CODEX=false
INSTALL_GEMINI=false
INSTALL_ALL_AGENTS=false

# Skip flags
SKIP_TUI=false
SKIP_COMPLETIONS=false
NO_DAEMON_RESTART=false

# Detected values
OS=""
ARCH=""
SHELL_NAME=""
SHELL_RC=""

#------------------------------------------------------------------------------
# Utility functions
#------------------------------------------------------------------------------

info() { echo -e "${BLUE}${BOLD}==>${NC} $1"; }
success() { echo -e "  ${GREEN}✓${NC} $1"; }
warn() { echo -e "  ${YELLOW}!${NC} $1"; }
error() { echo -e "  ${RED}✗${NC} $1" >&2; }
die() { error "$1"; exit 1; }

confirm() {
    if [ "$AUTO_MODE" = true ]; then return 0; fi
    local prompt="$1 [Y/n] "
    read -r -p "$prompt" response
    case "$response" in
        [nN][oO]|[nN]) return 1 ;;
        *) return 0 ;;
    esac
}

ask_yes_no() {
    if [ "$AUTO_MODE" = true ]; then return 1; fi
    local prompt="$1 [y/N] "
    read -r -p "$prompt" response
    case "$response" in
        [yY][eE][sS]|[yY]) return 0 ;;
        *) return 1 ;;
    esac
}

command_exists() { command -v "$1" &>/dev/null; }

installed_orch_version() {
    local version_out
    version_out=$("${INSTALL_DIR}/orch" --version 2>/dev/null | head -n1 | awk '{print $NF}' || true)
    [ -n "$version_out" ] && echo "$version_out" || echo "unknown"
}

orch_daemon_running() {
    if command_exists pgrep && pgrep -f "[o]rch daemon run" >/dev/null 2>&1; then
        return 0
    fi

    if [ -x "${INSTALL_DIR}/orch" ]; then
        "${INSTALL_DIR}/orch" daemon status 2>/dev/null | grep -q "Status: running"
        return $?
    fi

    return 1
}

#------------------------------------------------------------------------------
# Detection functions
#------------------------------------------------------------------------------

detect_os() {
    case "$(uname -s)" in
        Darwin) OS="darwin" ;;
        Linux) OS="linux" ;;
        *) die "Unsupported OS: $(uname -s)" ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64) ARCH="amd64" ;;
        arm64|aarch64) ARCH="arm64" ;;
        *) die "Unsupported architecture: $(uname -m)" ;;
    esac
}

detect_shell() {
    SHELL_NAME="$(basename "${SHELL:-/bin/bash}")"
    case "$SHELL_NAME" in
        bash)
            [ -f "${HOME}/.bashrc" ] && SHELL_RC="${HOME}/.bashrc" || SHELL_RC="${HOME}/.bash_profile"
            ;;
        zsh) SHELL_RC="${HOME}/.zshrc" ;;
        fish) SHELL_RC="${HOME}/.config/fish/config.fish" ;;
        *) SHELL_RC="" ;;
    esac
}

get_latest_release() {
    local latest
    if command_exists curl; then
        latest=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
    elif command_exists wget; then
        latest=$(wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
    else
        die "Neither curl nor wget found."
    fi
    [ -z "$latest" ] && die "Could not determine latest release."
    echo "$latest"
}

#------------------------------------------------------------------------------
# Dependency checking
#------------------------------------------------------------------------------

check_dependencies() {
    info "Checking dependencies..."
    local has_all=true
    
    # curl or wget
    if command_exists curl; then
        success "curl $(curl --version 2>/dev/null | head -n1 | awk '{print $2}')"
    elif command_exists wget; then
        success "wget $(wget --version 2>/dev/null | head -n1 | awk '{print $3}')"
    else
        error "curl or wget required"
        has_all=false
    fi
    
    # git
    if command_exists git; then
        success "git $(git --version | awk '{print $3}')"
    else
        error "git not found (required)"
        has_all=false
    fi
    
    # tmux or zellij
    if command_exists tmux; then
        success "tmux $(tmux -V 2>/dev/null | awk '{print $2}' || echo 'installed')"
    elif command_exists zellij; then
        success "zellij installed"
    else
        warn "tmux or zellij not found (recommended)"
        echo "       Install: brew install tmux (macOS) / apt install tmux (Linux)"
    fi
    
    # Python 3.10+ (for orch-monitor)
    if command_exists python3; then
        local py_ver py_minor
        py_ver=$(python3 --version 2>/dev/null | awk '{print $2}')
        py_minor=$(echo "$py_ver" | cut -d. -f2)
        if [ "${py_minor:-0}" -ge 10 ]; then
            success "python $py_ver"
        else
            warn "python $py_ver (3.10+ needed for orch-monitor)"
            SKIP_TUI=true
        fi
    else
        warn "python3 not found (needed for orch-monitor)"
        SKIP_TUI=true
    fi
    
    # uv
    if command_exists uv; then
        success "uv $(uv --version 2>/dev/null | awk '{print $2}' || echo 'installed')"
    else
        warn "uv not found (needed for orch-monitor)"
    fi
    
    # Node.js/npm (for agents)
    if command_exists npm; then
        success "npm $(npm --version 2>/dev/null)"
    else
        warn "npm not found (needed for LLM agent CLIs)"
    fi
    
    [ "$has_all" = false ] && die "Missing required dependencies."
    echo ""
}

#------------------------------------------------------------------------------
# Installation functions
#------------------------------------------------------------------------------

install_orch_binary() {
    local version="$1"
    local binary_name="orch-${OS}-${ARCH}"
    local download_url="https://github.com/${REPO}/releases/download/${version}/${binary_name}"
    local checksums_url="https://github.com/${REPO}/releases/download/${version}/checksums.txt"
    local tmp_dir
    tmp_dir=$(mktemp -d)
    
    info "Downloading orch ${version}..."
    
    if command_exists curl; then
        curl -fsSL "$download_url" -o "${tmp_dir}/${binary_name}" || die "Failed to download orch"
        curl -fsSL "$checksums_url" -o "${tmp_dir}/checksums.txt" 2>/dev/null || true
    else
        wget -q "$download_url" -O "${tmp_dir}/${binary_name}" || die "Failed to download orch"
        wget -q "$checksums_url" -O "${tmp_dir}/checksums.txt" 2>/dev/null || true
    fi
    
    # Verify checksum
    if [ -f "${tmp_dir}/checksums.txt" ]; then
        local expected actual
        expected=$(grep "${binary_name}$" "${tmp_dir}/checksums.txt" 2>/dev/null | awk '{print $1}')
        if [ -n "$expected" ]; then
            if command_exists sha256sum; then
                actual=$(sha256sum "${tmp_dir}/${binary_name}" | awk '{print $1}')
            elif command_exists shasum; then
                actual=$(shasum -a 256 "${tmp_dir}/${binary_name}" | awk '{print $1}')
            fi
            if [ -n "$actual" ] && [ "$expected" = "$actual" ]; then
                success "Checksum verified"
            elif [ -n "$actual" ]; then
                die "Checksum mismatch!"
            fi
        fi
    fi
    
    mkdir -p "$INSTALL_DIR"
    chmod +x "${tmp_dir}/${binary_name}"
    mv "${tmp_dir}/${binary_name}" "${INSTALL_DIR}/orch"
    
    [ "$OS" = "darwin" ] && codesign --force --sign - "${INSTALL_DIR}/orch" 2>/dev/null || true
    
    success "Installed orch to ${INSTALL_DIR}/orch"
    rm -rf "$tmp_dir"
}

restart_daemon_after_upgrade() {
    if [ "$NO_DAEMON_RESTART" = true ]; then
        warn "Skipping daemon restart (--no-daemon-restart)"
        return 0
    fi

    if ! orch_daemon_running; then
        success "No running orch daemon found; restart not needed"
        return 0
    fi

    info "Restarting running orch daemon..."
    if "${INSTALL_DIR}/orch" daemon-restart; then
        success "Restarted orch daemon"
    else
        warn "Could not restart orch daemon automatically; run: ${INSTALL_DIR}/orch daemon-restart"
    fi
}

install_uv() {
    if command_exists uv; then return 0; fi
    
    info "Installing uv..."
    if command_exists curl; then
        curl -LsSf https://astral.sh/uv/install.sh | sh
    else
        wget -qO- https://astral.sh/uv/install.sh | sh
    fi
    
    # Source uv env
    [ -f "${HOME}/.local/bin/env" ] && . "${HOME}/.local/bin/env"
    export PATH="${HOME}/.local/bin:$PATH"
    
    command_exists uv && success "Installed uv" || { warn "Failed to install uv"; return 1; }
}

install_orch_monitor() {
    if [ "$SKIP_TUI" = true ]; then
        warn "Skipping orch-monitor (Python 3.10+ required)"
        return 0
    fi
    
    # Ensure uv
    if ! command_exists uv; then
        if [ "$AUTO_MODE" = true ]; then
            install_uv || return 0
        else
            if ask_yes_no "uv not found. Install it?"; then
                install_uv || return 0
            else
                warn "Skipping orch-monitor (uv required)"
                return 0
            fi
        fi
    fi
    
    info "Installing orch-monitor TUI..."
    if uv tool install --force "git+https://github.com/${REPO}#subdirectory=orch-monitor-tui" 2>/dev/null; then
        success "Installed orch-monitor"
    else
        warn "Could not install orch-monitor"
    fi
}

install_agent() {
    local name="$1"
    local package="$2"
    
    if ! command_exists npm; then
        warn "npm not found, skipping ${name}"
        return 1
    fi
    
    info "Installing ${name}..."
    if npm install -g "$package"; then
        success "Installed ${name}"
    else
        warn "Failed to install ${name} from npm package ${package}"
    fi
}

install_agents_interactive() {
    if ! command_exists npm; then
        warn "npm not found, skipping agent installation"
        return 0
    fi
    
    echo ""
    info "Optional: Install LLM CLI agents"
    echo "  These tools let orch run AI agents in your terminal."
    echo ""
    
    if ask_yes_no "Install OpenCode?"; then
        install_agent "opencode" "opencode-ai"
    fi
    
    if ask_yes_no "Install claude (Anthropic Claude Code)?"; then
        install_agent "claude" "@anthropic-ai/claude-code"
    fi
    
    if ask_yes_no "Install codex (OpenAI)?"; then
        install_agent "codex" "@openai/codex"
    fi
    
    if ask_yes_no "Install gemini (Google)?"; then
        install_agent "gemini" "@google/gemini-cli"
    fi
}

install_agents_auto() {
    if [ "$INSTALL_ALL_AGENTS" = true ]; then
        INSTALL_OPENCODE=true
        INSTALL_CLAUDE=true
        INSTALL_CODEX=true
        INSTALL_GEMINI=true
    fi
    
    [ "$INSTALL_OPENCODE" = true ] && install_agent "opencode" "opencode-ai"
    [ "$INSTALL_CLAUDE" = true ] && install_agent "claude" "@anthropic-ai/claude-code"
    [ "$INSTALL_CODEX" = true ] && install_agent "codex" "@openai/codex"
    [ "$INSTALL_GEMINI" = true ] && install_agent "gemini" "@google/gemini-cli"
}

setup_path() {
    case ":$PATH:" in
        *":${INSTALL_DIR}:"*)
            success "${INSTALL_DIR} already in PATH"
            return 0
            ;;
    esac
    
    [ -z "$SHELL_RC" ] && { warn "Add ${INSTALL_DIR} to PATH manually"; return 0; }
    
    grep -qF "$INSTALL_DIR" "$SHELL_RC" 2>/dev/null && {
        success "PATH configured in ${SHELL_RC}"
        return 0
    }
    
    info "Adding ${INSTALL_DIR} to PATH..."
    echo "" >> "$SHELL_RC"
    echo "# orch" >> "$SHELL_RC"
    if [ "$SHELL_NAME" = "fish" ]; then
        echo "fish_add_path ${INSTALL_DIR}" >> "$SHELL_RC"
    else
        echo "export PATH=\"${INSTALL_DIR}:\$PATH\"" >> "$SHELL_RC"
    fi
    success "Added to ${SHELL_RC}"
    warn "Run: source ${SHELL_RC}"
}

setup_completions() {
    [ "$SKIP_COMPLETIONS" = true ] && return 0
    
    "${INSTALL_DIR}/orch" completion bash &>/dev/null || {
        warn "Completions not available"
        return 0
    }
    
    info "Setting up shell completions..."
    case "$SHELL_NAME" in
        bash)
            mkdir -p "${HOME}/.local/share/bash-completion/completions"
            "${INSTALL_DIR}/orch" completion bash > "${HOME}/.local/share/bash-completion/completions/orch"
            success "Bash completions installed"
            ;;
        zsh)
            local zdir="${HOME}/.local/share/zsh/site-functions"
            mkdir -p "$zdir"
            "${INSTALL_DIR}/orch" completion zsh > "${zdir}/_orch"
            grep -qF "$zdir" "$SHELL_RC" 2>/dev/null || {
                echo "fpath=(${zdir} \$fpath)" >> "$SHELL_RC"
                echo "autoload -Uz compinit && compinit" >> "$SHELL_RC"
            }
            success "Zsh completions installed"
            ;;
        fish)
            mkdir -p "${HOME}/.config/fish/completions"
            "${INSTALL_DIR}/orch" completion fish > "${HOME}/.config/fish/completions/orch.fish"
            success "Fish completions installed"
            ;;
    esac
}

create_default_config() {
    [ -f "${CONFIG_DIR}/config.yaml" ] && {
        success "Config exists at ${CONFIG_DIR}/config.yaml"
        return 0
    }
    
    info "Creating default config..."
    mkdir -p "$CONFIG_DIR"
    cat > "${CONFIG_DIR}/config.yaml" << 'EOF'
# orch configuration
# https://github.com/proboscis/orch

agent_multiplexer: tmux

# issues.path is optional. When omitted, orch uses
# ~/.local/share/orch/<repo-id>.
# issues:
#   path: ~/repos/my-issues
EOF
    success "Created ${CONFIG_DIR}/config.yaml"
}

#------------------------------------------------------------------------------
# Uninstall
#------------------------------------------------------------------------------

uninstall_orch() {
    info "Uninstalling orch..."
    
    [ -f "${INSTALL_DIR}/orch" ] && { rm -f "${INSTALL_DIR}/orch"; success "Removed orch binary"; }
    
    command_exists uv && uv tool uninstall orch-monitor 2>/dev/null && success "Removed orch-monitor"
    
    rm -f "${HOME}/.local/share/bash-completion/completions/orch" 2>/dev/null
    rm -f "${HOME}/.local/share/zsh/site-functions/_orch" 2>/dev/null
    rm -f "${HOME}/.config/fish/completions/orch.fish" 2>/dev/null
    success "Removed completions"
    
    if [ -d "$CONFIG_DIR" ] && confirm "Remove config ${CONFIG_DIR}?"; then
        rm -rf "$CONFIG_DIR"
        success "Removed config"
    fi
    
    echo ""
    info "orch uninstalled."
    warn "Remove PATH entries from shell config manually if needed."
}

#------------------------------------------------------------------------------
# Main
#------------------------------------------------------------------------------

show_banner() {
    echo ""
    echo -e "${BOLD}  orch installer v${VERSION}${NC}"
    echo ""
}

show_plan() {
    local version="$1"
    echo -e "  Detected: ${BOLD}${OS} ${ARCH}${NC}"
    echo ""
    echo "  Will install:"
    echo "    - orch CLI ${version} -> ${INSTALL_DIR}/orch"
    [ "$SKIP_TUI" = false ] && echo "    - orch-monitor TUI (via uv)"
    [ -n "$SHELL_NAME" ] && [ "$SKIP_COMPLETIONS" = false ] && echo "    - shell completions for ${SHELL_NAME}"
    [ "$NO_DAEMON_RESTART" = false ] && echo "    - restart running orch daemon after upgrade"
    echo ""
}

show_help() {
    cat << EOF
orch installer v${VERSION}

Usage: install.sh [options]

Modes:
    (default)           Interactive mode - prompts for each component
    --auto, -y          Auto mode - no prompts, sensible defaults

Options:
    --with-opencode     Install opencode CLI (auto mode)
    --with-claude       Install claude CLI (auto mode)
    --with-codex        Install codex CLI (auto mode)
    --with-gemini       Install gemini CLI (auto mode)
    --all               Install all agent CLIs (auto mode)
    --uninstall         Uninstall orch
    --skip-tui          Skip orch-monitor TUI
    --skip-completions  Skip shell completions
    --no-daemon-restart Do not restart a running orch daemon after upgrade
    --install-dir DIR   Install directory (default: ~/.local/bin)
    -h, --help          Show this help

Examples:
    # Interactive install
    curl -sSL https://raw.githubusercontent.com/${REPO}/main/install.sh | bash

    # Auto install (no prompts)
    curl -sSL ... | bash -s -- --auto

    # Auto with specific agents
    curl -sSL ... | bash -s -- --auto --with-opencode --with-claude

    # Auto with all agents
    curl -sSL ... | bash -s -- --auto --all

    # Uninstall
    curl -sSL ... | bash -s -- --uninstall
EOF
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -y|--yes|--auto) AUTO_MODE=true; shift ;;
            --uninstall) UNINSTALL=true; shift ;;
            --with-opencode) INSTALL_OPENCODE=true; shift ;;
            --with-claude) INSTALL_CLAUDE=true; shift ;;
            --with-codex) INSTALL_CODEX=true; shift ;;
            --with-gemini) INSTALL_GEMINI=true; shift ;;
            --all) INSTALL_ALL_AGENTS=true; shift ;;
            --skip-tui) SKIP_TUI=true; shift ;;
            --skip-completions) SKIP_COMPLETIONS=true; shift ;;
            --no-daemon-restart) NO_DAEMON_RESTART=true; shift ;;
            --install-dir) INSTALL_DIR="$2"; shift 2 ;;
            -h|--help) show_help; exit 0 ;;
            *) die "Unknown option: $1" ;;
        esac
    done
}

main() {
    parse_args "$@"
    show_banner
    
    detect_os
    detect_arch
    detect_shell
    
    [ "$UNINSTALL" = true ] && { uninstall_orch; exit 0; }
    
    # Check existing installation
    if [ -f "${INSTALL_DIR}/orch" ]; then
        local cur_ver
        cur_ver=$(installed_orch_version)
        warn "orch already installed (${cur_ver})"
        confirm "Reinstall/upgrade?" || { echo "Aborted."; exit 0; }
    fi
    
    check_dependencies
    
    local version
    version=$(get_latest_release)
    
    show_plan "$version"
    confirm "Proceed?" || { echo "Aborted."; exit 0; }
    
    echo ""
    install_orch_binary "$version"
    restart_daemon_after_upgrade
    install_orch_monitor
    
    # Agent installation
    if [ "$AUTO_MODE" = true ]; then
        install_agents_auto
    else
        install_agents_interactive
    fi
    
    setup_path
    setup_completions
    create_default_config
    
    echo ""
    info "Done! Run 'orch --help' to get started."
    command_exists orch || warn "Restart shell or: source ${SHELL_RC}"
}

main "$@"
