#!/bin/bash
# deploy.sh — pull-based updater for Raspberry Pi (DietPi)
# Usage: GITHUB_TOKEN=ghp_xxx ./deploy.sh
#
# Fetches the latest GitHub Release asset and deploys it.
# Run this periodically via cron or systemd timer.

set -euo pipefail

REPO="AsutoshDalei/Dashboard"
ASSET="myapp-linux-arm64.tar.gz"
INSTALL_DIR="/opt/pi_portfolio"
SERVICE="pi_portfolio"

GITHUB_TOKEN="${GITHUB_TOKEN:-}"

if [ -z "$GITHUB_TOKEN" ]; then
  echo "ERROR: GITHUB_TOKEN is required."
  echo "Create a classic PAT (repo scope) or a fine-grained token (contents:read) at:"
  echo "  https://github.com/settings/tokens"
  echo ""
  echo "Usage: GITHUB_TOKEN=ghp_xxx $0"
  exit 1
fi

echo "Fetching latest release info for $REPO ..."

release_json=$(curl -sSfL \
  -H "Authorization: Bearer $GITHUB_TOKEN" \
  -H "Accept: application/vnd.github+json" \
  "https://api.github.com/repos/$REPO/releases/latest")

tag=$(echo "$release_json" | jq -r '.tag_name')
echo "Latest release: $tag"

asset_url=$(echo "$release_json" \
  | jq -r --arg name "$ASSET" '.assets[] | select(.name == $name) | .url')

if [ -z "$asset_url" ] || [ "$asset_url" = "null" ]; then
  echo "ERROR: Asset '$ASSET' not found in release $tag"
  exit 1
fi

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

echo "Downloading $ASSET ..."
curl -sSfL \
  -H "Authorization: Bearer $GITHUB_TOKEN" \
  -H "Accept: application/octet-stream" \
  -o "$tmpdir/$ASSET" \
  "$asset_url"

echo "Extracting to $INSTALL_DIR ..."
mkdir -p "$INSTALL_DIR"
tar -xzf "$tmpdir/$ASSET" -C "$INSTALL_DIR"

chmod +x "$INSTALL_DIR/pi_portfolio_arm64"

echo "Restarting $SERVICE service ..."
if systemctl is-active --quiet "$SERVICE" 2>/dev/null; then
  sudo systemctl restart "$SERVICE"
  echo "Service restarted."
else
  echo "WARNING: Service '$SERVICE' not found or not active."
  echo "Start it manually or set it up:"
  echo "  sudo systemctl enable --now $SERVICE"
fi

echo ""
echo "Deploy complete: $tag deployed to $INSTALL_DIR"