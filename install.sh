#!/bin/bash

set -e

GITHUB_REPO="xdevplatform/xurl"
PROGRAM_NAME="xurl"

# Install to ~/.local/bin by default (no sudo needed).
# Falls back to /usr/local/bin if run as root.
# Use `id -u` rather than `$EUID`: $EUID is bash-only and expands to empty under
# POSIX shells (e.g. dash, the default /bin/sh on Debian/Ubuntu), which silently
# picks the user path even when run as root via `curl ... | sh`.
if [ "$(id -u)" -eq 0 ]; then
    INSTALL_DIR="/usr/local/bin"
else
    INSTALL_DIR="${HOME}/.local/bin"
fi

print_message() {
    echo -e "\033[1;34m>> $1\033[0m"
}

print_error() {
    echo -e "\033[1;31mError: $1\033[0m"
    exit 1
}

detect_architecture() {
    local arch=$(uname -m)
    case $arch in
        x86_64)
            echo "x86_64"
            ;;
        aarch64|arm64)
            echo "arm64"
            ;;
        i386|i686)
            echo "i386"
            ;;
        *)
            print_error "Unsupported architecture: $arch"
            ;;
    esac
}

detect_os() {
    local os=$(uname -s)
    case $os in
        Linux)
            echo "Linux"
            ;;
        Darwin)
            echo "Darwin"
            ;;
        *)
            print_error "Unsupported operating system: $os"
            ;;
    esac
}

ensure_in_path() {
    case ":$PATH:" in
        *":${INSTALL_DIR}:"*) ;;
        *)
            print_message "${INSTALL_DIR} is not in your PATH."
            echo ""
            echo "  Add it by running:"
            echo ""
            echo "    echo 'export PATH=\"${INSTALL_DIR}:\$PATH\"' >> ~/.bashrc  # or ~/.zshrc"
            echo "    source ~/.bashrc"
            echo ""
            ;;
    esac
}

download_release() {
    local os=$1
    local arch=$2
    local binary_name="${PROGRAM_NAME}_${os}_${arch}.tar.gz"

    local download_url="https://github.com/${GITHUB_REPO}/releases/latest/download/${binary_name}"
    print_message "Downloading latest release: ${binary_name}..."
    local temp_dir=$(mktemp -d)
    trap 'rm -rf -- "$temp_dir"' EXIT
    if ! curl -fsSL "$download_url" -o "${temp_dir}/${binary_name}"; then
        print_error "Failed to download release"
    fi
    tar xzf "${temp_dir}/${binary_name}" -C "$temp_dir"
    mkdir -p "${INSTALL_DIR}"
    print_message "Installing to ${INSTALL_DIR}..."
    mv "${temp_dir}/${PROGRAM_NAME}" "${INSTALL_DIR}/"
    chmod +x "${INSTALL_DIR}/${PROGRAM_NAME}"
}

main() {
    print_message "Starting installation..."
    local os=$(detect_os)
    local arch=$(detect_architecture)
    download_release "$os" "$arch"
    print_message "Installation complete! You can now run '${PROGRAM_NAME}' from anywhere."
    ensure_in_path
}

main