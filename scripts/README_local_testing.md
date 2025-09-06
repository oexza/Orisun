# Local Testing for GitHub Actions

This directory contains scripts to help test GitHub Actions workflows locally without having to push to GitHub and wait for the workflow to run.

## Available Scripts

- `test_changelog.sh` - Tests basic changelog generation
- `test_github_output.sh` - Tests GitHub Actions multiline output format
- `test_workflow_changelog.sh` - Tests the exact workflow syntax for changelog generation
- `simulate_github_actions.sh` - Simulates the GitHub Actions environment

## Using Act for Local GitHub Actions Testing

[Act](https://github.com/nektos/act) is a tool that allows you to run GitHub Actions locally. This is the most accurate way to test your workflows without pushing to GitHub.

### Installation

On macOS, you can install Act using Homebrew:

```bash
brew install act
```

### Usage

1. Make sure you have Docker installed and running
2. Run a specific workflow:

```bash
# Run the release workflow
act -j release -e .github/workflows/act-event.json
```

3. Create an event file (`.github/workflows/act-event.json`) with the necessary context:

```json
{
  "release": {
    "tag_name": "v0.0.42"
  }
}
```

### Testing Just the Changelog Step

To test just the changelog generation step:

```bash
act -j release -s GITHUB_TOKEN=fake-token --step generate\ changelog
```

## Troubleshooting GitHub Actions Multiline Output

If you're having issues with multiline output in GitHub Actions, ensure:

1. The delimiter is on its own line with no leading or trailing spaces
2. There's a newline before the closing delimiter
3. The exact same delimiter is used for opening and closing
4. No invisible characters or encoding issues are present

Example of correct format:

```yaml
- name: Set multiline output
  run: |
    echo "output<<EOF" >> $GITHUB_OUTPUT
    echo "line 1" >> $GITHUB_OUTPUT
    echo "line 2" >> $GITHUB_OUTPUT
    echo "EOF" >> $GITHUB_OUTPUT
```

## Further Resources

- [GitHub Actions Documentation](https://docs.github.com/en/actions)
- [Act Documentation](https://github.com/nektos/act)
- [GitHub Actions Multiline Commands](https://docs.github.com/en/actions/using-workflows/workflow-commands-for-github-actions#multiline-strings)