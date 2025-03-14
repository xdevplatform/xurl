#!/bin/bash

set -e

INSTALL_DIR="/usr/local/bin"
GITHUB_REPO="santiagomed/xurl"
PROGRAM_NAME="xurl"

print_message() {
    echo -e "\033[1;34m>> $1\033[0m"
}

print_error() {
    echo -e "\033[1;31mError: $1\033[0m"
    exit 1
}

check_permissions() {
    if [ "$EUID" -ne 0 ]; then
        print_error "Please run with sudo privileges"
    fi
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

download_release() {
    local os=$1
    local arch=$2
    local binary_name="${PROGRAM_NAME}_${os}_${arch}.tar.gz"
    
    local download_url="https://github.com/${GITHUB_REPO}/releases/latest/download/${binary_name}"
    print_message "Downloading latest release: ${binary_name}..."
    local temp_dir=$(mktemp -d)
    trap 'rm -rf -- "$temp_dir"' EXIT
    if ! curl -L "$download_url" -o "${temp_dir}/${binary_name}"; then
        print_error "Failed to download release"
    fi  
    tar xzf "${temp_dir}/${binary_name}" -C "$temp_dir"
    print_message "Installing to ${INSTALL_DIR}..."
    mv "${temp_dir}/${PROGRAM_NAME}" "${INSTALL_DIR}/"
    chmod +x "${INSTALL_DIR}/${PROGRAM_NAME}"
}

main() {
    print_message "Starting installation..."
    check_permissions
    local os=$(detect_os)
    local arch=$(detect_architecture)
    download_release "$os" "$arch"
    print_message "Installation complete! You can now run '${PROGRAM_NAME}' from anywhere."
}

main