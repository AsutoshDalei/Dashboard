#!/bin/bash
set -e

echo "Building pi_dashboard..."

BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)
COMMIT_HASH=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

TAILWIND_VERSION="v3.4.17"

detect_platform() {
  case "$(uname -s)" in
    Darwin)
      case "$(uname -m)" in
        arm64) echo "macos-arm64" ;;
        x86_64) echo "macos-x64" ;;
      esac
      ;;
    Linux)
      case "$(uname -m)" in
        aarch64|arm64) echo "linux-arm64" ;;
        x86_64) echo "linux-x64" ;;
        armv7l) echo "linux-armv7" ;;
      esac
      ;;
  esac
}

TAILWIND_PLATFORM=$(detect_platform)
TAILWIND_BIN="tailwindcss-${TAILWIND_PLATFORM}"

if [ ! -f "$TAILWIND_BIN" ]; then
  echo "Downloading Tailwind CSS standalone CLI for ${TAILWIND_PLATFORM}..."
  curl -sL "https://github.com/tailwindlabs/tailwindcss/releases/download/${TAILWIND_VERSION}/${TAILWIND_BIN}" -o "$TAILWIND_BIN"
  chmod +x "$TAILWIND_BIN"
fi

echo "Building Tailwind CSS..."
./"$TAILWIND_BIN" -i web/static/input.css -o static/output.css --minify

echo "Cross-compiling for linux/arm64..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w -X main.buildTime=$BUILD_TIME -X main.commitHash=$COMMIT_HASH" -o pi_bundle/pi_portfolio_arm64

echo "Building for local machine..."
go build -ldflags="-s -w -X main.buildTime=$BUILD_TIME -X main.commitHash=$COMMIT_HASH" -o pi_bundle/pi_portfolio_normal

mkdir -p pi_bundle/templates/email_body
	cp templates/email_body/*.tmpl pi_bundle/templates/email_body/
	cp templates/email_templates.json pi_bundle/templates/
	cp templates/coverletter.tex.tmpl pi_bundle/templates/
	cp templates/system_prompts.json pi_bundle/
	cp resume.tex pi_bundle/
	cp resume.md pi_bundle/

find pi_bundle -name '.DS_Store' -delete
rm -f pi_bundle/pi_bundle.tar.gz

COPYFILE_DISABLE=1 tar -czf pi_bundle.tar.gz \
    --exclude='.DS_Store' \
    --exclude='._*' \
    --exclude='__MACOSX' \
    -C pi_bundle \
    .
mv pi_bundle.tar.gz pi_bundle/

echo ""
echo "Build successful!"
echo "  - pi_portfolio_arm64 (linux/arm64)"
echo "  - pi_portfolio_normal (local)"
echo "  - Tailwind CSS embedded in binary"