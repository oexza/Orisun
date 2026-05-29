#!/bin/bash

# This script helps with creating a new release of Orisun

set -e

usage() {
    cat <<EOF
Usage: $0 <version> [--notes <file>]

Examples:
  $0 1.0.0
  $0 1.0.0 --notes release-notes.md

Notes:
  When --notes is provided, the file becomes the annotated tag message.
  Markdown headings are preserved exactly in the tag message.
  The GitHub release workflow uses that tag message as the release notes.
EOF
}

if [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
    usage
    exit 0
fi

# Check if a version argument was provided
if [ -z "$1" ]; then
    echo "Error: No version specified"
    usage
    exit 1
fi

VERSION="$1"
NOTES_FILE=""
shift

while [ $# -gt 0 ]; do
    case "$1" in
        --notes|-n)
            if [ -z "$2" ]; then
                echo "Error: --notes requires a file path"
                usage
                exit 1
            fi
            NOTES_FILE="$2"
            shift 2
            ;;
        --help|-h)
            usage
            exit 0
            ;;
        *)
            echo "Error: Unknown argument: $1"
            usage
            exit 1
            ;;
    esac
done

# Validate version format (should be semver without 'v' prefix)
if [[ ! $VERSION =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9\.]+)?$ ]]; then
    echo "Error: Version must be in semver format (e.g., 1.0.0, 1.2.3-beta.1)"
    exit 1
fi

if [ -n "$NOTES_FILE" ]; then
    if [ ! -f "$NOTES_FILE" ]; then
        echo "Error: Release notes file not found: $NOTES_FILE"
        exit 1
    fi
    if [ ! -s "$NOTES_FILE" ]; then
        echo "Error: Release notes file is empty: $NOTES_FILE"
        exit 1
    fi
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
if [ -n "$NOTES_FILE" ]; then
    git tag -a "v$VERSION" --cleanup=verbatim -F "$NOTES_FILE"
else
    git tag -a "v$VERSION" -m "Release v$VERSION"
fi

echo "Pushing tag to remote..."
git push origin "v$VERSION"

echo "\nRelease v$VERSION has been tagged and pushed."
if [ -n "$NOTES_FILE" ]; then
    echo "Release notes were attached from: $NOTES_FILE"
else
    echo "No release notes file was provided; the workflow will generate notes from commits."
fi
echo "The GitHub Actions workflow will now build and publish the release."
echo "You can monitor the progress at: https://github.com/$(git config --get remote.origin.url | sed 's/.*github.com[:\/]\(.*\)\.git/\1/')/actions"
