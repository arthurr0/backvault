# Release process

Releases are cut from annotated tags on `master`. Everything else is automated: GoReleaser
builds the archives and writes the release notes, and a second job builds and pushes the
container image. This page is for whoever presses the button.

## 1. Before the tag

Make sure `master` is green and the tree is what you want to ship.

```bash
git switch master
git pull
make lint
make test
make build
```

Then:

1. Decide the version. Semantic versioning: a breaking change to the API, the configuration
   or the database schema is a major bump, new behaviour is a minor bump, fixes are a patch
   bump. While the project is on `0.x` a breaking change bumps the minor.
2. Update `CHANGELOG.md`. Move everything under the unreleased heading into a new
   `## [X.Y.Z] - YYYY-MM-DD` section, keeping the Added, Changed, Fixed, Removed and Security
   groups. Write it for someone upgrading, not for someone reading the diff.
3. Check that the documentation matches the release. In particular the version in the
   examples in [install.md](install.md) and the Status section of the README.
4. Commit the changelog on its own:

```bash
git add CHANGELOG.md
git commit -m "chore(release): 0.1.0"
git push
```

Wait for CI on that commit to pass before tagging.

## 2. Cut the tag

```bash
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

The tag must start with `v` and the rest must be a semantic version, because both workflows
match on `v*` and GoReleaser derives the version from it by stripping the `v`.

A tag with a hyphen in it, `v0.1.0-rc1` for example, is treated as a prerelease: GoReleaser
marks the GitHub release as a prerelease, and the container image does not get the `latest`
tag. Use that for anything you want people to try without recommending it.

## 3. What the workflows produce

`.github/workflows/release.yml` runs two jobs in parallel.

**`release`** checks out the full history, builds the panel through the GoReleaser before
hook (`cd web && npm ci && npm run build`), then builds the binary for `linux/amd64`,
`linux/arm64`, `darwin/amd64` and `darwin/arm64` with `CGO_ENABLED=0`. The version, the short
commit and the build date are stamped into `internal/version`, so `backvault version` in the
release binary reports the tag. It publishes a GitHub release at
<https://github.com/arthurr0/backvault/releases> carrying:

| Asset | What it is |
|---|---|
| `backvault_<version>_<os>_<arch>.tar.gz` | the binary with the panel embedded, plus `LICENSE`, `README.md` and `scripts/` |
| `checksums.txt` | the sha256 of every archive |

The release notes are generated from the commits since the previous tag, grouped by the
conventional commit prefix (`feat`, `fix`, `perf`, `docs`, and build and tooling changes
together), with merge commits left out. That is why commit messages follow the style in
[CONTRIBUTING.md](../CONTRIBUTING.md).

**`image`** builds `deploy/Dockerfile` for `linux/amd64` and `linux/arm64` with QEMU and
buildx and pushes to the GitHub Container Registry as `ghcr.io/arthurr0/backvault`. The tags
it publishes for `v0.1.0` are `0.1.0`, `0.1`, and `latest`. A prerelease tag gets the version
tags only. `VERSION`, `COMMIT` and `BUILD_DATE` go in as build arguments, so the image reports
the same version as the archives.

Both jobs use the repository `GITHUB_TOKEN`. No extra secret has to be configured: the release
job needs `contents: write` and the image job needs `packages: write`, and both are declared in
the workflow.

## 4. After the tag

Check the two workflow runs, then verify the artifacts yourself.

Checksums:

```bash
version=0.1.0
base=https://github.com/arthurr0/backvault/releases/download/v${version}

curl -fsSLO "${base}/backvault_${version}_linux_amd64.tar.gz"
curl -fsSLO "${base}/checksums.txt"
sha256sum -c checksums.txt --ignore-missing
```

The archive must be reported as `OK`. On macOS use `shasum -a 256 -c`.

The binary:

```bash
tar xzf "backvault_${version}_linux_amd64.tar.gz"
./backvault version
./backvault check
```

The image, on both architectures if you can:

```bash
docker run --rm ghcr.io/arthurr0/backvault:0.1.0 version
docker run --rm ghcr.io/arthurr0/backvault:latest check
docker buildx imagetools inspect ghcr.io/arthurr0/backvault:0.1.0
```

`imagetools inspect` should list an `amd64` and an `arm64` manifest.

The first push to `ghcr.io` creates the package as private. Make it public once, under the
package settings on GitHub, or nobody can pull `ghcr.io/arthurr0/backvault` without
authenticating.

Finally, walk the quick start in the README on a clean host with the published artifacts, not
with your working tree. The installer path is worth running once per release:

```bash
curl -fsSL https://raw.githubusercontent.com/arthurr0/backvault/master/deploy/install.sh -o install.sh
sudo bash install.sh --download
```

## 5. Testing the build without releasing

GoReleaser can do everything except publishing, locally:

```bash
go run github.com/goreleaser/goreleaser/v2@latest check
go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean --skip=publish,docker
```

The archives land in `dist/`, which is ignored by git. Delete it when you are done.

## 6. If a release goes wrong

Do not move a published tag. Fix the problem on `master` and cut the next patch version.

If a release has to disappear, delete the GitHub release and the tag, delete the image tags in
the package settings, and say why in the changelog of the release that replaces it. Anyone who
already pulled keeps what they pulled, so treat a published tag as permanent and only remove
one when it is actively harmful.
