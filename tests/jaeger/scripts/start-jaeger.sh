#!/bin/bash

# Jaeger Development Setup Script
# This script starts Jaeger All-in-One for local development with OpenTelemetry

set -e

DOWNLOAD_NAME="darwin-amd64"

# Configuration

# Example download url
# https://github.com/jaegertracing/jaeger/releases/download/v2.15.1/jaeger-2.15.1-darwin-arm64.tar.gz

# This is the jaeger v2 version
JAEGER_VERSION="2.15.1"

# Get the project root directory, handling both direct execution and symlink execution
if [ -L "${BASH_SOURCE[0]}" ]; then
    # Script is accessed via symlink, resolve the real script path first
    REAL_SCRIPT="$(readlink "${BASH_SOURCE[0]}")"
    PROJECT_ROOT="$(cd "$(dirname "$REAL_SCRIPT")/.." && pwd)"
else
    # Script is run directly from scripts/ directory
    PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fi
JAEGER_DIR="$PROJECT_ROOT/bin/jaeger-${JAEGER_VERSION}-${DOWNLOAD_NAME}"
# Jaeger v2 uses 'jaeger' binary
JAEGER_BINARY="$JAEGER_DIR/jaeger"
JAEGER_DOWNLOAD_URL="https://github.com/jaegertracing/jaeger/releases/download/v${JAEGER_VERSION}/jaeger-${JAEGER_VERSION}-${DOWNLOAD_NAME}.tar.gz"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🔍 Jaeger Development Setup${NC}"
echo

# Function to check latest Jaeger version
check_latest_version() {
    echo -e "${BLUE}🔍 Checking latest Jaeger version...${NC}"
    # Get both v1 and v2 versions from the latest release
    release_info=$(curl -s https://api.github.com/repos/jaegertracing/jaeger/releases/latest)
    v2_version=$(echo "$release_info" | grep '"name":.*v2\.' | sed -E 's/.*v2\.([0-9]+\.[0-9]+).*/2.\1/' | head -1)

    latest="$v2_version"

    if [ -n "$latest" ] && [ "$latest" != "$JAEGER_VERSION" ]; then
        echo -e "${YELLOW}💡 A newer version is available: $latest (current: $JAEGER_VERSION)${NC}"
        echo "To update, run: $0 --update"
        echo
    fi
}

# Function to download and install Jaeger
download_jaeger() {
    local version=${1:-$JAEGER_VERSION}
    local dir="$PROJECT_ROOT/bin/jaeger-${version}-${DOWNLOAD_NAME}"

    # For v2 releases, we need to get the corresponding v1 tag from the release
    local download_tag
    if [[ "$version" == 2.* ]]; then
        # For v2, get the v1 tag from the latest release
        download_tag=$(curl -s https://api.github.com/repos/jaegertracing/jaeger/releases/latest | grep '"tag_name":' | sed -E 's/.*"v([^"]+)".*/\1/')
    else
        download_tag="$version"
    fi

    local url="https://github.com/jaegertracing/jaeger/releases/download/v${download_tag}/jaeger-${version}-${DOWNLOAD_NAME}.tar.gz"

    echo -e "${BLUE}📥 Downloading Jaeger v${version}...${NC}"
    echo -e "${BLUE}🔗 Using download tag: v${download_tag}${NC}"

    # Create temp file
    local temp_file=$(mktemp)

    # Download with progress
    if curl -L -o "$temp_file" "$url"; then
        echo -e "${GREEN}✅ Downloaded successfully${NC}"

        # Extract to jaeger directory
        echo -e "${BLUE}📦 Extracting...${NC}"
        # Ensure bin directory exists
        mkdir -p "$PROJECT_ROOT/bin"
        (cd "$PROJECT_ROOT/bin" && tar -xzf "$temp_file")

        # Cleanup old version if it exists and is different
        if [ -d "$JAEGER_DIR" ] && [ "$dir" != "$JAEGER_DIR" ]; then
            echo -e "${YELLOW}🧹 Removing old version...${NC}"
            rm -rf "$JAEGER_DIR"
        fi

        # Update global variables to point to new version
        JAEGER_DIR="$dir"
        # Jaeger v2 uses 'jaeger' binary instead of 'jaeger-all-in-one'
        JAEGER_BINARY="$JAEGER_DIR/jaeger"

        # Cleanup
        rm -f "$temp_file"

        echo -e "${GREEN}✅ Jaeger v${version} installed to $JAEGER_DIR${NC}"
    else
        echo -e "${RED}❌ Failed to download Jaeger v${version}${NC}"
        rm -f "$temp_file"
        exit 1
    fi
}

# Handle command line arguments
case "${1:-}" in
    --update)
        echo -e "${BLUE}🔄 Updating Jaeger...${NC}"
        release_info=$(curl -s https://api.github.com/repos/jaegertracing/jaeger/releases/latest)
        v1_version=$(echo "$release_info" | grep '"tag_name":' | sed -E 's/.*"v([^"]+)".*/\1/')
        v2_version=$(echo "$release_info" | grep '"name":.*v2\.' | sed -E 's/.*v2\.([0-9]+\.[0-9]+).*/2.\1/' | head -1)

        # Use v2 as the latest if available, otherwise v1
        latest="$v2_version"
        if [ -z "$latest" ]; then
            latest="$v1_version"
        fi

        if [ -n "$latest" ]; then
            download_jaeger "$latest"
            # Update the version in this script
            sed -i '' "s/JAEGER_VERSION=\"[^\"]*\"/JAEGER_VERSION=\"$latest\"/" "$0"
            echo -e "${GREEN}✅ Updated to Jaeger v${latest}${NC}"
            echo "You can now run $0 to start the new version"
        else
            echo -e "${RED}❌ Failed to get latest version${NC}"
        fi
        exit 0
        ;;
    --version)
        echo "Current Jaeger version: $JAEGER_VERSION"
        if [ -f "$JAEGER_BINARY" ]; then
            echo "Binary exists: $JAEGER_BINARY"
        else
            echo "Binary not found: $JAEGER_BINARY"
        fi
        exit 0
        ;;
    --help|-h)
        echo "Jaeger Development Setup Script"
        echo ""
        echo "Usage: $0 [OPTIONS]"
        echo ""
        echo "Options:"
        echo "  (no args)     Start Jaeger with current version"
        echo "  --all-fvt     Start Jaeger and run feature tests (make all-fvt)"
        echo "  --update      Download and install latest Jaeger version"
        echo "  --version     Show current version information"
        echo "  --help        Show this help message"
        echo ""
        echo "Endpoints when running:"
        echo "  • Jaeger UI: http://localhost:16686"
        echo "  • OTLP gRPC: localhost:4317"
        echo "  • OTLP HTTP: localhost:4318"
        echo "  • Query gRPC: localhost:16685"
        echo "  • Remote Sampling HTTP: localhost:5778"
        echo "  • Remote Sampling gRPC: localhost:5779"
        exit 0
        ;;
esac

# Check if we're starting Jaeger
echo -e "${BLUE}🔍 Jaeger Development Setup (v${JAEGER_VERSION})${NC}"
echo

# Always check and display latest version before starting
echo -e "${BLUE}🔍 Checking latest Jaeger version...${NC}"
release_info=$(curl -s https://api.github.com/repos/jaegertracing/jaeger/releases/latest)
v1_version=$(echo "$release_info" | grep '"tag_name":' | sed -E 's/.*"v([^"]+)".*/\1/')
v2_version=$(echo "$release_info" | grep '"name":.*v2\.' | sed -E 's/.*v2\.([0-9]+\.[0-9]+).*/2.\1/' | head -1)

# Use v2 as the latest if available, otherwise v1
latest="$v2_version"
if [ -z "$latest" ]; then
    latest="$v1_version"
fi

if [ -n "$latest" ]; then
    echo -e "${GREEN}📋 Latest available version: v${latest}${NC}"
    echo -e "${GREEN}📋 Currently configured: v${JAEGER_VERSION}${NC}"
    if [ "$latest" != "$JAEGER_VERSION" ]; then
        echo -e "${YELLOW}💡 A newer version is available! Run '$0 --update' to upgrade${NC}"
    else
        echo -e "${GREEN}✅ You have the latest version${NC}"
    fi
else
    echo -e "${YELLOW}⚠️  Could not fetch latest version information${NC}"
    echo -e "${GREEN}📋 Currently configured: v${JAEGER_VERSION}${NC}"
fi
echo

# Check if Jaeger binary exists
if [ ! -f "$JAEGER_BINARY" ]; then
    echo -e "${RED}❌ Jaeger binary not found at $JAEGER_BINARY${NC}"
    echo -e "${BLUE}📥 Automatically downloading Jaeger v${JAEGER_VERSION}...${NC}"
    download_jaeger
fi

# Check if Jaeger is already running
if lsof -i :16686 >/dev/null 2>&1; then
    echo -e "${YELLOW}⚠️  Jaeger appears to be already running on port 16686${NC}"
    echo "Do you want to kill existing processes and restart? (y/n)"
    read -r response
    if [[ "$response" == "y" || "$response" == "Y" ]]; then
        echo "Killing existing Jaeger processes..."
        pkill -f "jaeger.*--collector" || true  # v2 jaeger binary
        sleep 2
    else
        echo "Exiting..."
        exit 0
    fi
fi

echo -e "${GREEN}🚀 Starting Jaeger...${NC}"

# Jaeger v2 uses different startup approach - no command line flags needed
# It uses default configuration with memory storage and OTLP support enabled by default
echo -e "${BLUE}🔧 Using Jaeger v2 with default configuration${NC}"
# Jaeger v2 runs with default ports:
# - UI: 16686
# - OTLP gRPC: 4317
# - OTLP HTTP: 4318
# - Query gRPC: 16685
"$JAEGER_BINARY" &

JAEGER_PID=$!
echo -e "${GREEN}✅ Jaeger started with PID $JAEGER_PID${NC}"

# Wait a moment for startup
sleep 3

# Check if Jaeger is running
if kill -0 $JAEGER_PID 2>/dev/null; then
    echo -e "${GREEN}🎉 Jaeger is running successfully!${NC}"
    echo
    echo -e "${BLUE}📋 Service Information:${NC}"
    echo "  • Jaeger UI: http://localhost:16686"
    echo "  • OTLP gRPC Endpoint: localhost:4317"
    echo "  • OTLP HTTP Endpoint: localhost:4318"
    echo "  • Query gRPC: localhost:16685"
    echo "  • Remote Sampling HTTP: localhost:5778"
    echo "  • Remote Sampling gRPC: localhost:5779"
    echo ""

    echo -e "${YELLOW}💡 Tips:${NC}"
    echo "  • Access the Jaeger UI at http://localhost:16686"
    echo "  • Your Go application is configured to send traces to localhost:4317"
    echo "  • Jaeger v2 uses standard OTLP ports (4317 gRPC, 4318 HTTP)"
    echo "  • Press Ctrl+C to stop Jaeger"
    echo

    # Wait for interrupt
    trap 'echo -e "\n${YELLOW}🛑 Stopping Jaeger...${NC}"; kill $JAEGER_PID; exit 0' INT
    wait $JAEGER_PID
else
    echo -e "${RED}❌ Failed to start Jaeger${NC}"
    exit 1
fi
