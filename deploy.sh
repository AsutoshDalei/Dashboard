#!/bin/bash
# Usage: GITHUB_TOKEN=ghp_xxx ./deploy.sh [dest_dir]
# Downloads the latest GitHub Release asset and extracts it into a folder.
# Default destination: ./pi_portfolio

set -euo pipefail

REPO="AsutoshDalei/Dashboard"
ASSET="myapp-linux-arm64.tar.gz"
DEST="${1:-pi_portfolio}"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TOKEN_FILE="$SCRIPT_DIR/.github_token"

# Priority: env var > .github_token file
if [ -n "${GITHUB_TOKEN:-}" ]; then
  TOKEN="$GITHUB_TOKEN"
elif [ -f "$TOKEN_FILE" ]; then
  TOKEN=$(cat "$TOKEN_FILE" | tr -d '[:space:]')
else
  echo "ERROR: No GitHub token found."
  echo "Set it once: echo 'ghp_xxx' > '$TOKEN_FILE'"
  echo "Or pass per-run: GITHUB_TOKEN=ghp_xxx $0"
  exit 1
fi

echo "Fetching latest release for $REPO ..."

release_json=$(curl -sSfL \
  -H "Authorization: Bearer $TOKEN" \
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
  -H "Authorization: Bearer $TOKEN" \
  -H "Accept: application/octet-stream" \
  -o "$tmpdir/$ASSET" \
  "$asset_url"

echo "Extracting to $DEST/ ..."
mkdir -p "$DEST"
tar -xzf "$tmpdir/$ASSET" -C "$DEST"
chmod +x "$DEST/pi_portfolio_arm64"

echo ""
echo "Done: $tag extracted to $DEST/"
echo ""

# Ensure fontconfig is installed (required by tectonic for PDF generation)
if ! dpkg -s fontconfig >/dev/null 2>&1; then
  echo "Installing fontconfig (required by tectonic)..."
  sudo apt-get update -qq && sudo apt-get install -y fontconfig
fi

echo "Run: cd $DEST && ./pi_portfolio_arm64"