#!/bin/bash

# This script tests the release-notes selection used by the GitHub Actions workflow.

set -e

# Create a temporary file to simulate GITHUB_OUTPUT
TEMP_OUTPUT_FILE=$(mktemp)
echo "Using temporary file: $TEMP_OUTPUT_FILE"

VERSION=${1:-0.0.0}
RELEASE_NOTES_INPUT=${RELEASE_NOTES_INPUT:-}

# Get the previous tag
PREVIOUS_TAG=$(git describe --tags --abbrev=0 HEAD^ 2>/dev/null || echo "")
echo "Previous tag: '$PREVIOUS_TAG'"
echo "Version: '$VERSION'"

echo "Testing release notes generation..."
echo "==========================================="

cat > release-notes.md <<EOF
## Orisun v${VERSION}

EOF

if [ -n "$RELEASE_NOTES_INPUT" ]; then
  printf "%s\n" "$RELEASE_NOTES_INPUT" >> release-notes.md
else
  TAG_NOTES=$(git tag -l "v${VERSION}" --format='%(contents)' | sed '/^-----BEGIN PGP SIGNATURE-----$/,$d')
  if [ -n "$TAG_NOTES" ] && [ "$TAG_NOTES" != "Release v${VERSION}" ]; then
    printf "%s\n" "$TAG_NOTES" >> release-notes.md
  fi
fi

if [ "$(wc -l < release-notes.md | tr -d ' ')" -le 2 ]; then
  if [ -z "$PREVIOUS_TAG" ]; then
    echo "Initial release" >> release-notes.md
  else
    echo "## Changes" >> release-notes.md
    echo "" >> release-notes.md
    git log --pretty=format:"* %s (%h)" "$PREVIOUS_TAG"..HEAD >> release-notes.md
    echo "" >> release-notes.md
  fi
fi

echo "path=release-notes.md" >> "$TEMP_OUTPUT_FILE"

echo "Generated release notes:"
echo "------------------------------------------"
cat release-notes.md

echo ""
echo "Validating format:"
echo "------------------------------------------"

if grep -q "^path=release-notes.md$" "$TEMP_OUTPUT_FILE" && grep -q "^## Orisun v${VERSION}$" release-notes.md; then
  echo "✅ Release notes file generated"
else
  echo "❌ Invalid format detected"
  exit 1
fi

# Clean up
rm "$TEMP_OUTPUT_FILE"
rm release-notes.md

echo ""
echo "==========================================="
echo "Test completed!"
