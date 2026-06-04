#!/bin/bash
# Usage: GITHUB_TOKEN=ghp_xxx ./deploy.sh [dest_dir]
# Downloads the latest GitHub Release asset and extracts it into a folder.
# Default destination: ./pi_portfolio

set -euo pipefail

REPO="AsutoshDalei/Dashboard"
ASSET="myapp-linux-arm64.tar.gz"
DEST="${1:-pi_portfolio}"

GITHUB_TOKEN="${GITHUB_TOKEN:-}"

if [ -z "$GITHUB_TOKEN" ]; then
  echo "ERROR: GITHUB_TOKEN is required."
  echo "Create a PAT (repo scope or contents:read) at:"
  echo "  https://github.com/settings/tokens"
  echo ""
  echo "Usage: GITHUB_TOKEN=ghp_xxx $0 [dest_dir]"
  exit 1
fi

echo "Fetching latest release for $REPO ..."

release_json=$(curl -sSfL \
  -H "Authorization: Bearer $GITHUB_TOKEN" \
  -H "Accept: application/vnd.github+json" \
  "https://api.github.com/repos/$REPO/releases/latest")

tag=$(echo "$release_json" | python3 -c "import sys,json; print(json.load(sys.stdin)['tag_name'])")
echo "Latest release: $tag"

asset_url=$(echo "$release_json" | python3 -c "
import sys, json
data = json.load(sys.stdin)
for a in data.get('assets', []):
    if a['name'] == '$ASSET':
        print(a['url'])
        break
")

if [ -z "$asset_url" ]; then
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

echo "Extracting to $DEST/ ..."
mkdir -p "$DEST"
tar -xzf "$tmpdir/$ASSET" -C "$DEST"

echo ""
echo "Done: $tag extracted to $DEST/"
echo "Run: cd $DEST && ./pi_portfolio_arm64"