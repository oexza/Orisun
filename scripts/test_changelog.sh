#!/bin/bash

# This script tests the changelog generation locally

set -e

# Function to generate changelog
generate_changelog() {
  PREVIOUS_TAG=$(git describe --tags --abbrev=0 HEAD^ 2>/dev/null || echo "")
  
  if [ -z "$PREVIOUS_TAG" ]; then
    echo "Initial release"
  else
    echo "Changes since $PREVIOUS_TAG:"
    echo ""
    git log --pretty=format:"* %s (%h)" $PREVIOUS_TAG..HEAD
  fi
}

# Run the changelog generation
echo "Testing changelog generation..."
echo "=========================="
echo ""
generate_changelog

echo ""
echo "=========================="
echo "Test completed successfully!"