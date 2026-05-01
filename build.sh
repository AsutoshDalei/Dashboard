#!/bin/bash
# Build script for cross-compiling pi_portfolio for Raspberry Pi Zero 2 W

set -e

echo "Building pi_portfolio for Raspberry Pi Zero 2 W..."

# The Raspberry Pi Zero 2 W features a 64-bit quad-core ARM Cortex-A53 processor.
# Below compiles to a statically-linked 64-bit ARM binary.
# Note: If your Pi installation is 32-bit (legacy Raspberry Pi OS), 
# change GOARCH=arm64 to GOARCH=arm and add GOARM=7.

GOOS=linux GOARCH=arm64 go build -o pi_bundle/pi_portfolio_arm64
go build -o pi_bundle/pi_portfolio_normal
mkdir -p pi_bundle/templates
cp templates/email_body.tmpl pi_bundle/templates/email_body.tmpl
cp templates/coverletter.tex.tmpl pi_bundle/templates/coverletter.tex.tmpl

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
