#!/bin/bash

# This script simulates the GitHub Actions environment for local testing

set -e

# Create a temporary directory for the simulation
TEMP_DIR=$(mktemp -d)
GITHUB_OUTPUT="$TEMP_DIR/github_output"
touch "$GITHUB_OUTPUT"

echo "Simulating GitHub Actions environment..."
echo "GITHUB_OUTPUT=$GITHUB_OUTPUT"
echo ""

# Function to run the workflow steps
simulate_workflow() {
  echo "Running workflow steps..."
  echo "==========================================="
  
  # Get the previous tag (same as in the workflow)
  PREVIOUS_TAG=$(git describe --tags --abbrev=0 HEAD^ 2>/dev/null || echo "")
  echo "Previous tag: '$PREVIOUS_TAG'"
  
  # Generate changelog (exact code from the workflow)
  echo "Generating changelog..."
  if [ -z "$PREVIOUS_TAG" ]; then
    echo "changelog=Initial release" >> "$GITHUB_OUTPUT"
  else
    echo "changelog<<EOF" >> "$GITHUB_OUTPUT"
    echo "Changes since $PREVIOUS_TAG:" >> "$GITHUB_OUTPUT"
    echo "" >> "$GITHUB_OUTPUT"
    git log --pretty=format:"* %s (%h)" "$PREVIOUS_TAG"..HEAD >> "$GITHUB_OUTPUT"
    echo "" >> "$GITHUB_OUTPUT"
    echo "EOF" >> "$GITHUB_OUTPUT"
  fi
  
  echo "Changelog generated."
}

# Run the simulation
simulate_workflow

# Display the results
echo ""
echo "Content of GITHUB_OUTPUT file:"
echo "------------------------------------------"
cat "$GITHUB_OUTPUT"

echo ""
echo "Validating format:"
echo "------------------------------------------"

# Check if the format is valid
if grep -q "^changelog=" "$GITHUB_OUTPUT"; then
  echo "✅ Simple format detected (Initial release)"
elif grep -q "^changelog<<EOF" "$GITHUB_OUTPUT" && grep -q "^EOF$" "$GITHUB_OUTPUT"; then
  echo "✅ Multiline format with proper delimiters detected"
else
  echo "❌ Invalid format detected"
  echo "The format doesn't match GitHub Actions requirements"
fi

# Clean up
rm -rf "$TEMP_DIR"

echo ""
echo "==========================================="
echo "Simulation completed!"

echo ""
echo "To fix issues in GitHub Actions:"
echo "1. Make sure the EOF delimiter is on its own line"
echo "2. Ensure there's a newline before the EOF delimiter"
echo "3. Check for any invisible characters or encoding issues"
echo "4. Try using act (https://github.com/nektos/act) to run GitHub Actions locally"