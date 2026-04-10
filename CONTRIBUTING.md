# Contributing to AKB

Thank you for your interest in contributing! This document covers how to set up your development environment, run tests, and submit changes.

## Prerequisites

- **Go 1.25+** — [install](https://go.dev/dl/)
- **rclone** — only needed if you work on remote KB mounting; see [docs/rclone-setup.md](docs/rclone-setup.md) for installation instructions

## Development setup

```bash
git clone https://github.com/bkosm/akb.git
cd akb
cp .envrc.example .envrc
# edit .envrc if needed, then:
source .envrc
```

## Running the server locally

```bash
# Local config backend
./bin/stdio.sh local

# S3 config backend
./bin/stdio.sh s3 --bucket my-bucket
```

## Make targets

All build, test, and lint tasks are available via `make`:

| Target | Description |
|--------|-------------|
| `make build` | Compile the binary to `bin/akb` |
| `make test` | Run unit tests with race detector |
| `make lint` | Run golangci-lint |
| `make fmt` | Format all Go code |
| `make vet` | Run go vet |
| `make clean` | Remove compiled binary |

Run `make test` before submitting a PR. Run `make lint` to check for style issues.

## Code style

- All code must be formatted with `gofmt` (`make fmt` enforces this)
- `go vet` must pass (`make vet`)
- Every exported type, function, and method must have a Go doc comment
- No `log.Printf` / `log.Fatal` — use `slog` with structured key-value pairs

## Commit conventions

This project uses [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>: <short description>

Why: <reason the change is needed>

What: <what the change does>
```

Common types: `feat`, `fix`, `refactor`, `test`, `docs`, `build`, `ci`, `chore`.

## Pull request process

1. Fork the repo and create a branch from `main`
2. Make your changes, add or update tests
3. Ensure `make test` and `make lint` pass
4. Open a PR — fill in the PR template
5. A maintainer will review and merge

## Reporting bugs and requesting features

Please use the GitHub issue templates:

- [Bug report](https://github.com/bkosm/akb/issues/new?template=bug_report.md)
- [Feature request](https://github.com/bkosm/akb/issues/new?template=feature_request.md)
