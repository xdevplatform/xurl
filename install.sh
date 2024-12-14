#!/bin/bash

# Exit immediately if any command fails
set -e

# Default installation directory
INSTALL_DIR="/usr/local/bin"
GITHUB_REPO="santiagomed/xurl"
PROGRAM_NAME="xurl"

# Print colorful messages
print_message() {
    echo -e "\033[1;34m>> $1\033[0m"
}

print_error() {
    echo -e "\033[1;31mError: $1\033[0m"
    exit 1
}

# Check if running with sudo
check_permissions() {
    if [ "$EUID" -ne 0 ]; then
        print_error "Please run with sudo privileges"
    fi
}

# Detect system architecture
detect_architecture() {
    local arch=$(uname -m)
    case $arch in
        x86_64)
            echo "x86_64"
            ;;
        aarch64|arm64)
            echo "aarch64"
            ;;
        *)
            print_error "Unsupported architecture: $arch"
            ;;
    esac
}

# Detect operating system
detect_os() {
    local os=$(uname -s)
    case $os in
        Linux)
            echo "linux"
            ;;
        Darwin)
            echo "darwin"
            ;;
        *)
            print_error "Unsupported operating system: $os"
            ;;
    esac
}

# Download the latest release
download_release() {
    local os=$1
    local arch=$2
    
    # Construct binary name
    local binary_name="${PROGRAM_NAME}-${os}-${arch}.tar.gz"
    local download_url="https://github.com/${GITHUB_REPO}/releases/latest/download/${binary_name}"
    
    print_message "Downloading latest release for ${os}-${arch}..."
    
    # Create temporary directory
    local temp_dir=$(mktemp -d)
    trap 'rm -rf -- "$temp_dir"' EXIT
    
    # Download and extract
    if ! curl -L "$download_url" -o "${temp_dir}/${binary_name}"; then
        print_error "Failed to download release"
    fi
    
    tar xzf "${temp_dir}/${binary_name}" -C "$temp_dir"
    
    # Install binary
    print_message "Installing to ${INSTALL_DIR}..."
    mv "${temp_dir}/${PROGRAM_NAME}" "${INSTALL_DIR}/"
    chmod +x "${INSTALL_DIR}/${PROGRAM_NAME}"
}

# Main installation process
main() {
    print_message "Starting installation..."
    
    # Check if running with necessary permissions
    check_permissions
    
    # Detect system details
    local os=$(detect_os)
    local arch=$(detect_architecture)
    
    # Download and install
    download_release "$os" "$arch"
    
    print_message "Installation complete! You can now run '${PROGRAM_NAME}' from anywhere."
}

# Run main function
main