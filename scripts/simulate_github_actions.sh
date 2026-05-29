#!/bin/bash

# This script simulates the release-notes portion of the GitHub Actions release workflow.

set -e

TEMP_DIR=$(mktemp -d)
GITHUB_OUTPUT="$TEMP_DIR/github_output"
RELEASE_NOTES_FILE="$TEMP_DIR/release-notes.md"
VERSION=${1:-0.0.0}
RELEASE_NOTES_INPUT=${RELEASE_NOTES_INPUT:-}

touch "$GITHUB_OUTPUT"

echo "Simulating release notes workflow..."
echo "GITHUB_OUTPUT=$GITHUB_OUTPUT"
echo "VERSION=$VERSION"
echo ""

PREVIOUS_TAG=$(git describe --tags --abbrev=0 HEAD^ 2>/dev/null || echo "")
echo "Previous tag: '$PREVIOUS_TAG'"

cat > "$RELEASE_NOTES_FILE" <<EOF
## Orisun v${VERSION}

EOF

if [ -n "$RELEASE_NOTES_INPUT" ]; then
  printf "%s\n" "$RELEASE_NOTES_INPUT" >> "$RELEASE_NOTES_FILE"
else
  TAG_NOTES=$(git tag -l "v${VERSION}" --format='%(contents)' | sed '/^-----BEGIN PGP SIGNATURE-----$/,$d')
  if [ -n "$TAG_NOTES" ] && [ "$TAG_NOTES" != "Release v${VERSION}" ]; then
    printf "%s\n" "$TAG_NOTES" >> "$RELEASE_NOTES_FILE"
  fi
fi

if [ "$(wc -l < "$RELEASE_NOTES_FILE" | tr -d ' ')" -le 2 ]; then
  if [ -z "$PREVIOUS_TAG" ]; then
    echo "Initial release" >> "$RELEASE_NOTES_FILE"
  else
    echo "## Changes" >> "$RELEASE_NOTES_FILE"
    echo "" >> "$RELEASE_NOTES_FILE"
    git log --pretty=format:"* %s (%h)" "$PREVIOUS_TAG"..HEAD >> "$RELEASE_NOTES_FILE"
    echo "" >> "$RELEASE_NOTES_FILE"
  fi
fi

echo "path=$RELEASE_NOTES_FILE" >> "$GITHUB_OUTPUT"

echo ""
echo "Generated release notes:"
echo "------------------------------------------"
cat "$RELEASE_NOTES_FILE"

echo ""
echo "GITHUB_OUTPUT:"
echo "------------------------------------------"
cat "$GITHUB_OUTPUT"

if grep -q "^path=$RELEASE_NOTES_FILE$" "$GITHUB_OUTPUT"; then
  echo "✅ Simulation completed successfully"
else
  echo "❌ Simulation failed"
  rm -rf "$TEMP_DIR"
  exit 1
fi

rm -rf "$TEMP_DIR"
