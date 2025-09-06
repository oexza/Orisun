#!/bin/bash

# This script tests the exact GitHub Actions workflow changelog generation step

set -e

# Create a temporary file to simulate GITHUB_OUTPUT
TEMP_OUTPUT_FILE=$(mktemp)
echo "Using temporary file: $TEMP_OUTPUT_FILE"

# Get the previous tag
PREVIOUS_TAG=$(git describe --tags --abbrev=0 HEAD^ 2>/dev/null || echo "")
echo "Previous tag: '$PREVIOUS_TAG'"

# Test the exact workflow syntax
echo "Testing the exact workflow syntax..."
echo "==========================================="

# This is the exact code from the workflow
if [ -z "$PREVIOUS_TAG" ]; then
  echo "changelog=Initial release" >> "$TEMP_OUTPUT_FILE"
else
  echo "changelog<<EOF" >> "$TEMP_OUTPUT_FILE"
  echo "Changes since $PREVIOUS_TAG:" >> "$TEMP_OUTPUT_FILE"
  echo "" >> "$TEMP_OUTPUT_FILE"
  git log --pretty=format:"* %s (%h)" "$PREVIOUS_TAG"..HEAD >> "$TEMP_OUTPUT_FILE"
  echo "" >> "$TEMP_OUTPUT_FILE"
  echo "EOF" >> "$TEMP_OUTPUT_FILE"
fi

echo "Content of simulated GITHUB_OUTPUT file:"
echo "------------------------------------------"
cat "$TEMP_OUTPUT_FILE"

echo ""
echo "Validating format:"
echo "------------------------------------------"

# Check if the format is valid
if grep -q "^changelog=" "$TEMP_OUTPUT_FILE"; then
  echo "✅ Simple format detected (Initial release)"
elif grep -q "^changelog<<EOF" "$TEMP_OUTPUT_FILE" && grep -q "^EOF$" "$TEMP_OUTPUT_FILE"; then
  echo "✅ Multiline format with proper delimiters detected"
else
  echo "❌ Invalid format detected"
  echo "The format doesn't match GitHub Actions requirements"
fi

# Clean up
rm "$TEMP_OUTPUT_FILE"

echo ""
echo "==========================================="
echo "Test completed!"

echo ""
echo "Recommendation for GitHub Actions:"
echo "Make sure the EOF delimiter is on its own line with no leading or trailing spaces"
echo "Make sure there's a newline before the EOF delimiter"