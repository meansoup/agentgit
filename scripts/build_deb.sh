#!/bin/bash
# scripts/build_deb.sh

set -e

APP_NAME="agentgit"
VERSION="0.1.0"
ARCH=$(go env GOARCH)
# Map Go arch to Debian arch
if [ "$ARCH" == "amd64" ]; then
    DEB_ARCH="amd64"
elif [ "$ARCH" == "arm64" ]; then
    DEB_ARCH="arm64"
else
    DEB_ARCH=$ARCH
fi

PACKAGE_DIR="${APP_NAME}_${VERSION}_${DEB_ARCH}"

echo "Building binary for Linux/${ARCH}..."
GOOS=linux GOARCH=$ARCH go build -o ${APP_NAME} main.go

echo "Creating Debian package structure..."
mkdir -p ${PACKAGE_DIR}/usr/bin
mkdir -p ${PACKAGE_DIR}/DEBIAN

cp ${APP_NAME} ${PACKAGE_DIR}/usr/bin/

# Create control file
cat <<EOT > ${PACKAGE_DIR}/DEBIAN/control
Package: ${APP_NAME}
Version: ${VERSION}
Section: utils
Priority: optional
Architecture: ${DEB_ARCH}
Maintainer: meansoup <meansoup@github.com>
Description: AgentGit is a TUI for checking AI agent code changes.
 Timeline-based TUI for reviewing AI agent-led development.
EOT

echo "Building .deb package..."
dpkg-deb --build ${PACKAGE_DIR}

echo "Cleaning up..."
rm -rf ${PACKAGE_DIR}
rm ${APP_NAME}

echo "Done! Created ${PACKAGE_DIR}.deb"
