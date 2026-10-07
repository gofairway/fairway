# Contributing to Fairway

Thanks for contributing to Fairway.

Fairway is a Stellar payment-corridor health monitor, and contributions are welcome across measurement logic, verification, persistence, API development, testing, documentation, and deployment.

## Development setup

Clone the repository and enter the project directory:

```bash
git clone https://github.com/gofairway/fairway.git
cd fairway
```

The recommended development environment uses Docker Compose:

```bash
docker compose up
```

Before making changes, make sure the application starts successfully and that the existing tests pass.

If you are working on a specific component, keep changes focused on that component where possible.

## Code style and quality

Keep code simple, readable, and consistent with the existing project structure.

Before opening a pull request:

* Run the test suite.
* Make sure Docker Compose still starts successfully.
* Keep new behavior covered by tests where practical.
* Avoid unrelated changes in the same commit or pull request.
* Update documentation when a change affects configuration, behavior, APIs, or project usage.

Fairway relies on CI to catch regressions, so a pull request should leave the repository in a buildable and testable state.

## Measurement changes

Changes to corridor measurement logic deserve particular care because they affect the meaning of Fairway's historical data.

If you change:

* trade-size calculation,
* Horizon pathfinding behavior,
* FX benchmarking,
* loss calculation,
* state thresholds,
* asset verification,
* or state-transition logic,

include tests and explain the behavioral change in the pull request.

When adding or changing a seeded corridor, follow the verification methodology in:

[`docs/corridor-verification.md`](docs/corridor-verification.md)

Do not describe a corridor as usable, degraded, or unusable without explaining the measurement basis.

## Pull requests

To contribute code:

1. Fork the repository to your own GitHub account.
2. Clone your fork and create a new branch for your change:
```bash
   git clone https://github.com/<your-username>/fairway.git
   cd fairway
   git checkout -b your-branch-name
```
3. Make your changes on that branch, committing as described below.
4. Push the branch to your fork:
```bash
   git push origin your-branch-name
```
5. Open a pull request from your fork's branch against `gofairway/fairway`'s `main` branch.

Before opening the pull request:

1. Make sure your branch contains only the changes related to the contribution.
2. Run the relevant tests.
3. Check that the application still builds and starts.
4. Update documentation if necessary.
5. Write a clear pull request description explaining **what changed and why**.

Keep pull requests focused and reasonably small where possible. Smaller changes are easier to review, test, and maintain.

Maintainers may request changes before a pull request is merged. Please address review feedback or explain why an alternative approach is preferable.

## Commits

Use clear commit messages that describe the change.

For example:

```text
Add corridor history endpoint
Fix health state transition race
Add NGNC corridor verification
Improve measurement tests
```

Avoid vague messages such as:

```text
update
fix stuff
changes
more work
```

A commit should represent a meaningful, understandable change.

## Reporting issues

When reporting a bug, include enough information to reproduce it.

Where relevant, include:

* what you expected to happen;
* what actually happened;
* the corridor or asset involved;
* the trade size or measurement conditions;
* relevant logs or API responses;
* steps to reproduce the problem.

Do not include secrets, private credentials, or sensitive production data in issues or pull requests.

## Documentation

Documentation is part of the project.

If a contribution changes how Fairway is installed, configured, measured, queried, or operated, update the relevant documentation alongside the code.

For corridor verification and seeded asset methodology, see:

[`docs/corridor-verification.md`](docs/corridor-verification.md)

For project-level information and usage, see:

[`README.md`](README.md)

## Code of Conduct

Please follow the project's [Code of Conduct](CODE_OF_CONDUCT.md).

Contributors are expected to communicate respectfully, give constructive feedback, and help maintain an inclusive environment for everyone working on Fairway.

## Questions

If you are unsure about an implementation, open an issue or start a discussion before making a large change.

For substantial changes, explaining the problem and proposed approach first can save time for both contributors and maintainers.
