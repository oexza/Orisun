#!/bin/bash

# This script tests the GitHub Actions multiline output format locally

set -e

# Create a temporary file to simulate GITHUB_OUTPUT
TEMP_OUTPUT_FILE=$(mktemp)

# Function to generate changelog in GitHub Actions format
generate_changelog_github_format() {
  PREVIOUS_TAG=$(git describe --tags --abbrev=0 HEAD^ 2>/dev/null || echo "")
  
  if [ -z "$PREVIOUS_TAG" ]; then
    echo "changelog=Initial release" >> "$TEMP_OUTPUT_FILE"
  else
    echo "changelog<<EOF" >> "$TEMP_OUTPUT_FILE"
    git log --pretty=format:"* %s (%h)" $PREVIOUS_TAG..HEAD >> "$TEMP_OUTPUT_FILE"
    echo "" >> "$TEMP_OUTPUT_FILE"
    echo "EOF" >> "$TEMP_OUTPUT_FILE"
  fi
}

# Run the changelog generation
echo "Testing GitHub Actions multiline output format..."
echo "==========================================="

generate_changelog_github_format

echo "Content of simulated GITHUB_OUTPUT file:"
echo "------------------------------------------"
cat "$TEMP_OUTPUT_FILE"

echo ""
echo "Extracting the changelog value:"
echo "------------------------------------------"

# Extract the changelog value from the output file
if grep -q "changelog=Initial release" "$TEMP_OUTPUT_FILE"; then
  echo "Initial release"
else
  # Extract content between the delimiters
  sed -n '/^changelog<<EOF$/,/^EOF$/p' "$TEMP_OUTPUT_FILE" | sed '1d;$d'
fi

# Clean up
rm "$TEMP_OUTPUT_FILE"

echo ""
echo "==========================================="
echo "Test completed successfully!"