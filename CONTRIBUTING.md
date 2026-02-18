# Contributing to pulumi-tool-drift-adopter

First, thanks for contributing to Pulumi and helping make it better. We appreciate the help!
This repository is one of many across the Pulumi ecosystem and we welcome contributions to them all.

## Code of Conduct

Please make sure to read and observe our [Contributor Code of Conduct](./CODE-OF-CONDUCT.md).

## Communications

You are welcome to join the [Pulumi Community Slack](https://slack.pulumi.com/) for questions and a community of like-minded folks.
We discuss features and file bugs on GitHub via [Issues](https://github.com/pulumi-labs/pulumi-tool-drift-adopter/issues) as well as [Discussions](https://github.com/pulumi-labs/pulumi-tool-drift-adopter/discussions).

### Issues

Feel free to pick up any existing issue that looks interesting to you or fix a bug you stumble across while using Pulumi. No matter the size, we welcome all improvements.

### Feature Work

For larger features, we'd appreciate it if you open a [new issue](https://github.com/pulumi-labs/pulumi-tool-drift-adopter/issues/new) before investing a lot of time so we can discuss the feature together.
Please also be sure to browse [current issues](https://github.com/pulumi-labs/pulumi-tool-drift-adopter/issues) to make sure your issue is unique, to lighten the triage burden on our maintainers.
Finally, please limit your pull requests to contain only one feature at a time. Separating feature work into individual pull requests helps speed up code review and reduces the barrier to merge.

## Developing

### Setting up your development environment

**Prerequisites:** Go 1.24+, [just](https://github.com/casey/just)

```bash
# Install development tools (golangci-lint, actionlint)
$ just install-tools

# Build the binary
$ just build

# Run tests
$ just test
```

## Submitting a Pull Request

For contributors we use the [standard fork based workflow](https://gist.github.com/Chaser324/ce0505fbed06b947d962): Fork this repository, create a topic branch, and when ready, open a pull request from your fork.

Before you open a pull request, make sure all lint checks pass:

```bash
$ just lint
```

If you see formatting failures, you can auto-fix them by running:

```bash
$ just lint-fix
```

We require a changelog entry for all PRs. Add an entry to [`CHANGELOG.md`](./CHANGELOG.md) under the `[Unreleased]` section describing your change.

### Pull Request Descriptions

Write clear, informative PR descriptions that help reviewers understand your changes and provide effective feedback. A good PR description serves as documentation for your changes and helps maintainers understand the impact and context.

**Important**: We use squash merge, so your PR description will become the git commit message. Write it accordingly.

The PR description should include:

- What: A clear summary of what changes are being made (often the PR title is sufficient)
- **Why: The motivation, context, or problem being solved. This is the most important part of the description.**
- How: Optionally a brief explanation of the approach taken (if not obvious). For detailed implementation explanations, consider adding comments in the code instead.
- References: Links to related issues, RFCs, or previous PRs

For PR titles, follow the same guidelines as [changelog messages](#changelog-messages). Use the active imperative form and be descriptive about what the change accomplishes.

### Changelog messages

Changelog notes are written in the active imperative form.  They should not end with a period.  The simple rule is to pretend the message starts with "This change will ..."

Good examples for changelog entries are:
- Exit immediately from state edit when no change was made
- Fix root and program paths to always be absolute

Here's some examples of what we're trying to avoid:
- Fixes a bug
- Adds a feature
- Feature now does something

### Pulumi employees

Pulumi employees have write access to Pulumi repositories and should push directly to branches rather than forking the repository. Tests can run directly without approval for PRs based on branches rather than forks.

Please ensure that you nest your branches under a unique identifier such as your name (e.g. `refs/heads/pulumipus/cool_feature`).

## Getting Help

We're sure there are rough edges and we appreciate you helping out. If you want to talk with other folks in the Pulumi community (including members of the Pulumi team) come hang out in the `#contribute` channel on the [Pulumi Community Slack](https://slack.pulumi.com/).
