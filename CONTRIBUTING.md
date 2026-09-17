# Contributing to Backvault

Thanks for taking the time. Backvault is a small project with a narrow scope, so the fastest
way to get a change merged is to open an issue first and agree on the shape of it before you
write code.

## Development setup

You need Go 1.27 and Node 22 or newer. The repository ships a `mise.toml`, so if you use
[mise](https://mise.jdx.dev) the Go version is pinned for you:

```bash
git clone https://github.com/arthurr0/backvault.git
cd backvault
mise install
```

Without mise, install Go 1.27 and Node 22 yourself and check them:

```bash
go version
node --version
```

Build and run it:

```bash
make web          # npm ci and npm run build in web/
make build        # bin/backvault with the panel embedded
make dev-server   # go run ./cmd/backvault serve --data-dir ./data
make dev-web      # vite dev server on :5173, proxies /api to :8080
```

Working on the panel alone does not need the Go backend. `npm run mock` in `web/` starts a
Node only mock API with realistic data, and `npm run dev` picks it up.

### Make targets

| Target | What it does |
|---|---|
| `make web` | installs the web dependencies and builds `web/dist` |
| `make build` | builds `bin/backvault` with the panel embedded |
| `make test` | `go test ./...` |
| `make race` | `go test -race ./...` |
| `make lint` | `go vet`, `gofmt -l`, `npm run typecheck`, `npm run lint`, `bash -n` over every script |
| `make fmt` | `gofmt -w` over the Go sources |
| `make docker` | builds the image from `deploy/Dockerfile` |
| `make clean` | removes `bin/`, `web/dist` and the local binary |

Run `make lint` and `make test` before you open a pull request. CI runs the same checks plus
a container image build.

## Project layout

`SPEC.md` is the product and engineering specification: the domain model, the pipeline
stages, the driver contracts, the API shape and the definition of done. Read the section that
covers the area you are changing before you start. The short version of the tree:

```text
cmd/backvault/     the CLI and server entry point
internal/core/     domain types, the shape of the API
internal/source/   source driver interface and registry
internal/dest/     destination driver interface and registry
internal/notify/   notifier interface and registry
internal/engine/   run queue, pipeline, scheduler, retention, restore
internal/server/   HTTP API, middleware, SSE, embedded panel
internal/store/    SQLite storage and migrations
web/               Vite, React and TypeScript admin panel
scripts/           standalone bash tooling for hosts without Backvault
deploy/            Dockerfile, compose file, systemd unit, installer
docs/              the documentation
```

Adding a driver means implementing the interface in `internal/source` or `internal/dest`,
registering it from `internal/drivers`, adding its field metadata so the panel can render the
form, and writing the page under `docs/sources/` or `docs/destinations/`.

## Coding rules

These are not negotiable, because they keep the diff readable and the codebase consistent.

- **No comments.** Not in Go, TypeScript, YAML, shell or the Dockerfile. A shebang is the only
  exception. Name things so the code reads without them, and put the explanation in `docs/`
  or in the commit message where it belongs.
- **English only**, everywhere: code, identifiers, log lines, API messages, panel strings,
  documentation and commit messages.
- Go code is formatted with `gofmt` and passes `go vet`. No third party linter is required.
- TypeScript passes `tsc --noEmit` and `eslint` with the config in `web/`. No `any` unless
  there is genuinely nothing better, and no disabled rules without a reason you can defend.
- Shell scripts are bash with `set -Eeuo pipefail`, pass `bash -n`, and use the helpers in
  `scripts/lib/common.sh` rather than reinventing logging and retries.
- Error messages are lowercase, start with the thing that failed, and say what to do next
  when that is knowable.
- New behaviour comes with a test. New user visible behaviour comes with a documentation
  page or a section in an existing one.

## Tests

```bash
make test
make race
```

Driver integration tests talk to real services in containers and are skipped unless you opt
in:

```bash
BACKVAULT_TEST_DOCKER=1 go test ./internal/source/... ./internal/dest/...
```

They start PostgreSQL, MariaDB, MongoDB, Redis, `atmoz/sftp` and MinIO containers, use them,
and remove them afterwards. Docker and rootless Podman both work. With Podman, expose the
socket and point Docker's environment variable at it:

```bash
systemctl --user start podman.socket
export DOCKER_HOST=unix://$XDG_RUNTIME_DIR/podman/podman.sock
BACKVAULT_TEST_DOCKER=1 go test ./internal/dest/s3/...
```

The first run pulls the images, so give it a few minutes. If a test fails because a port is
taken, check for a leftover container from an interrupted run.

## Commit messages

Conventional commits, because the release notes are generated from them:

```text
feat(dest/s3): support storage class on upload
fix(engine): release the job lock when a pre-command fails
docs(install): document the download path of the installer
```

Types in use: `feat`, `fix`, `perf`, `docs`, `refactor`, `test`, `build`, `ci`, `chore`. The
scope is the package or the area, for example `engine`, `server`, `web`, `scripts`,
`source/postgres`. Write the subject in the imperative, under 72 characters, no trailing
period. Put the reasoning in the body if it is not obvious from the diff.

Do not add trailers naming tools or generators.

## Pull requests

Before you open one:

- [ ] `make lint` passes
- [ ] `make test` passes, and `make race` if you touched the engine or the store
- [ ] the branch is rebased on `master` and the history is tidy
- [ ] commits follow the message style above
- [ ] no comments were added to any file
- [ ] documentation under `docs/` is updated for any user visible change
- [ ] `CHANGELOG.md` has an entry under the unreleased heading for anything a user would notice
- [ ] screenshots are attached if the change touches the panel

Keep a pull request to one topic. A refactor and a feature in the same branch take much
longer to review than the two of them separately.

## Reporting bugs and asking for features

Use the issue forms. The bug form asks for the version from `backvault version`, how you
installed it, which drivers are involved and the relevant log lines, because without those
the first reply is always the same three questions.

Security problems do not go in an issue. See [SECURITY.md](SECURITY.md).

## Code of conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md). By participating you
agree to it.

## License

Contributions are accepted under the [MIT License](LICENSE) that covers the project.
