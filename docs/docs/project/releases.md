---
title: Releases
description: Publish binaries, Docker images, and curated release notes.
---

Create a normal release from `main`:

```bash
./scripts/release.sh 1.2.3
```

Attach curated release notes to the GitHub release:

```bash
./scripts/release.sh 1.2.3 --notes release-notes.md
```

The release script stores notes verbatim in the annotated git tag, including markdown headings.

The GitHub release workflow uses release notes in this order:

1. Manual `release_notes` input from `workflow_dispatch`.
2. Annotated tag message from `scripts/release.sh --notes`.
3. Generated commit log since the previous tag.

## Release checklist

Before tagging:

1. Run the Go test suite.
2. Build the docs site with `bun run build` from `docs/`.
3. Confirm release notes describe user-facing behavior, migrations, and image tags.
4. Create the annotated tag with `scripts/release.sh`.

## Binary Assets

Each release publishes standalone binaries for Linux, macOS, and Windows. Use these when deploying Orisun directly as a process instead of through Docker.

| Asset pattern | Backend |
| --- | --- |
| `orisun-<os>-<arch>` | All backends |
| `orisun-pg-<os>-<arch>` | PostgreSQL only |
| `orisun-sqlite-<os>-<arch>` | SQLite only |

Linux and macOS binaries should be marked executable after download:

```bash
chmod +x ./orisun-sqlite
```

## Docker Tags

| Tag | Backend |
| --- | --- |
| `orexza/orisun:latest` | All backends |
| `orexza/orisun:pg` | PostgreSQL only |
| `orexza/orisun:sqlite` | SQLite only |
| `orexza/orisun:<version>` | All backends for a release version |
| `orexza/orisun:<version>-pg` | PostgreSQL-only release version |
| `orexza/orisun:<version>-sqlite` | SQLite-only release version |
