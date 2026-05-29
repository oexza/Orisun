#!/bin/bash

# Verifies that the workflow refreshes an annotated tag before reading notes.

set -e

TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT

ORIGIN="$TEMP_DIR/origin.git"
SOURCE="$TEMP_DIR/source"
CHECKOUT="$TEMP_DIR/checkout"

git init --bare -q "$ORIGIN"
git init -q "$SOURCE"
git -C "$SOURCE" config user.email "test@example.com"
git -C "$SOURCE" config user.name "Release Test"

echo "test" > "$SOURCE/file.txt"
git -C "$SOURCE" add file.txt
git -C "$SOURCE" commit -q -m "commit fallback text"
git -C "$SOURCE" branch -M main

cat > "$SOURCE/release-notes.md" <<EOF
## Highlights

- Uses annotated tag notes.
EOF

git -C "$SOURCE" tag -a v0.0.1 --cleanup=verbatim -F release-notes.md
git -C "$SOURCE" remote add origin "$ORIGIN"
git -C "$SOURCE" push -q origin main v0.0.1

git init -q "$CHECKOUT"
git -C "$CHECKOUT" remote add origin "$ORIGIN"
git -C "$CHECKOUT" fetch -q origin main
git -C "$CHECKOUT" checkout -q FETCH_HEAD
git -C "$CHECKOUT" tag v0.0.1 HEAD

if git -C "$CHECKOUT" tag -l v0.0.1 --format='%(contents)' | grep -q "commit fallback text"; then
  :
else
  echo "Expected lightweight checkout tag to expose commit fallback text"
  exit 1
fi

git -C "$CHECKOUT" fetch --force origin "refs/tags/v0.0.1:refs/tags/v0.0.1"

if git -C "$CHECKOUT" tag -l v0.0.1 --format='%(contents)' | grep -q '^## Highlights$'; then
  echo "Release workflow tag fetch restores annotated release notes"
else
  echo "Release workflow tag fetch did not restore annotated release notes"
  exit 1
fi
