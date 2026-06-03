#!/bin/bash
# Build script for cross-compiling pi_portfolio for Raspberry Pi Zero 2 W

set -e

echo "Building pi_portfolio for Raspberry Pi Zero 2 W..."

BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)
COMMIT_HASH=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w -X main.buildTime=$BUILD_TIME -X main.commitHash=$COMMIT_HASH" -o pi_bundle/pi_portfolio_arm64
go build -ldflags="-s -w -X main.buildTime=$BUILD_TIME -X main.commitHash=$COMMIT_HASH" -o pi_bundle/pi_portfolio_normal
mkdir -p pi_bundle/templates
mkdir -p pi_bundle/data
cp templates/email_body.tmpl pi_bundle/templates/email_body.tmpl
cp templates/coverletter.tex.tmpl pi_bundle/templates/coverletter.tex.tmpl

# Clean macOS artifacts from bundle
find pi_bundle -name '.DS_Store' -delete

echo ""
echo "Build successful!"
echo ""
echo "The binary has been placed in: pi_bundle/pi_portfolio_arm64"
echo ""
echo "The pi_bundle/ folder is ready for deployment. It contains:"
echo "  - pi_portfolio_arm64      (the compiled binary)"
echo "  - pi_portfolio_normal     (local binary for same machine)"
echo "  - .env                    (configuration - set your ACCESS_PASSKEY)"
echo "  - credentials.json        (Google OAuth credentials)"
echo "  - token.json              (Gmail API token)"
echo "  - templates/              (editable runtime templates)"
echo ""
echo "Optional: Copy your resume PDF to pi_bundle/ if you want it attached to emails."
echo ""
echo "To deploy: zip the pi_bundle folder and transfer to your Pi Zero 2 W."