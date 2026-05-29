#!/bin/bash

# Verifies that release notes stored in annotated tags preserve markdown.

set -e

TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT

cd "$TEMP_DIR"
git init -q
git config user.email "test@example.com"
git config user.name "Release Test"

echo "test" > file.txt
git add file.txt
git commit -q -m "initial"

cat > release-notes.md <<EOF
## Highlights

- Preserves markdown headings.

## Notes

- Keeps curated release notes readable.
EOF

git tag -a v0.0.1 --cleanup=verbatim -F release-notes.md

if git tag -l v0.0.1 --format='%(contents)' | grep -q '^## Highlights$'; then
  echo "Release notes tag cleanup preserves markdown headings"
else
  echo "Release notes tag cleanup stripped markdown headings"
  exit 1
fi
