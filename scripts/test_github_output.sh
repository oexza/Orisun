#!/bin/bash

# This script tests the GitHub Actions output used for release notes.

set -e

TEMP_DIR=$(mktemp -d)
GITHUB_OUTPUT="$TEMP_DIR/github_output"
touch "$GITHUB_OUTPUT"

RELEASE_NOTES_FILE="$TEMP_DIR/release-notes.md"
VERSION=${1:-0.0.0}

cat > "$RELEASE_NOTES_FILE" <<EOF
## Orisun v${VERSION}

Test release notes.
EOF

echo "path=$RELEASE_NOTES_FILE" >> "$GITHUB_OUTPUT"

echo "Content of simulated GITHUB_OUTPUT file:"
echo "------------------------------------------"
cat "$GITHUB_OUTPUT"

echo ""
echo "Release notes:"
echo "------------------------------------------"
cat "$RELEASE_NOTES_FILE"

echo ""
echo "Validating format:"
echo "------------------------------------------"

if grep -q "^path=$RELEASE_NOTES_FILE$" "$GITHUB_OUTPUT"; then
  echo "✅ Release notes path output detected"
else
  echo "❌ Invalid release notes path output"
  rm -rf "$TEMP_DIR"
  exit 1
fi

rm -rf "$TEMP_DIR"

echo ""
echo "==========================================="
echo "Test completed successfully!"
