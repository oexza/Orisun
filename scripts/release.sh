#!/bin/bash

# This script helps with creating a new release of Orisun

set -e

# Check if a version argument was provided
if [ -z "$1" ]; then
    echo "Error: No version specified"
    echo "Usage: $0 <version>"
    echo "Example: $0 1.0.0"
    exit 1
fi

VERSION="$1"

# Validate version format (should be semver without 'v' prefix)
if [[ ! $VERSION =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9\.]+)?$ ]]; then
    echo "Error: Version must be in semver format (e.g., 1.0.0, 1.2.3-beta.1)"
    exit 1
fi

# Ensure we're on the main branch
CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
if [ "$CURRENT_BRANCH" != "main" ]; then
    echo "Error: You must be on the main branch to create a release"
    exit 1
fi

# Ensure the working directory is clean
if [ -n "$(git status --porcelain)" ]; then
    echo "Error: Working directory is not clean. Commit or stash changes before creating a release."
    exit 1
fi

# Pull latest changes
echo "Pulling latest changes from main..."
git pull origin main

# Create and push the tag
echo "Creating tag v$VERSION..."
git tag -a "v$VERSION" -m "Release v$VERSION"

echo "Pushing tag to remote..."
git push origin "v$VERSION"

echo "\nRelease v$VERSION has been tagged and pushed."
echo "The GitHub Actions workflow will now build and publish the release."
echo "You can monitor the progress at: https://github.com/$(git config --get remote.origin.url | sed 's/.*github.com[:\/]\(.*\)\.git/\1/')/actions"