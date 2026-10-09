# pe-challenge-p1-go-app

[![CI](https://github.com/clickbg/pe-challenge-p1-go-app/actions/workflows/ci.yml/badge.svg)](https://github.com/clickbg/pe-challenge-p1-go-app/actions/workflows/ci.yml)
[![Release](https://github.com/clickbg/pe-challenge-p1-go-app/actions/workflows/release.yml/badge.svg)](https://github.com/clickbg/pe-challenge-p1-go-app/actions/workflows/release.yml)

Phase 1 of the Mondoo Platform Engineer challenge. A minimal Go web server (`hello-mondoo`) plus the CI and release pipelines that build, check, sign and publish it.

## The app

```
$ make run
$ curl localhost:8080/
Hello from Mondoo Engineer!
```

- Listens on `HTTP_PORT`, default `8080`.
- An invalid `HTTP_PORT` (not a number, or outside 1-65535) exits with code 1 instead of falling back to 8080. A typo in a deployment should fail loudly, not show up later as a failing probe on the wrong port.
- `GET /` returns the greeting. Other paths return 404, other methods 405.
- Handles SIGTERM with a graceful shutdown, so Kubernetes rollouts don't drop in-flight requests.
- Server timeouts are set (`ReadHeaderTimeout` etc.). Go's defaults have none, which leaves the server open to slowloris-style connections. gosec flags this too.
- Logs JSON to stdout. Version is injected at build time and logged on startup.
- Standard library only, no third-party modules.

## CI

`.github/workflows/ci.yml` runs on pushes to `main` and on pull requests.

| Job | What it does |
| --- | --- |
| `semgrep` | SAST with the `p/golang` and `p/secrets` rulesets. Any finding fails the job. SARIF goes to the Security tab even when the scan fails, so findings show up inline on PRs. |
| `lint` | golangci-lint v2 with the `standard` set plus gosec, revive, errorlint, bodyclose, noctx and a few cheap extras. Config in `.golangci.yml`. |
| `test` | `go test -race` with coverage. The per-function table is written to the job summary. |
| `govulncheck` | Checks the module and the Go stdlib against the Go vulnerability database. |
| `build` | Runs only when all four above pass. Builds a static binary, starts it with `HTTP_PORT=18080` and checks the response body, then uploads it as an artifact (7 days). |

The workflow also has a `workflow_call` trigger, so the release pipeline reuses it as a gate instead of keeping a second copy of the checks.

## Releases

`.github/workflows/release.yml` runs when a semver tag is pushed (`v1.2.3`, or `v1.2.3-rc.1` for a pre-release).

1. Calls `ci.yml`. Nothing is built for release unless every check passes.
2. Builds a matrix of linux, darwin and windows for amd64 and arm64. `CGO_ENABLED=0`, `-trimpath`, version from the tag. `fail-fast` is on, so one broken target stops the release.
3. Collects the binaries and generates one SPDX SBOM for all of them with syft.
4. Writes `SHA256SUMS` over every asset, SBOM included, and signs it keyless with cosign using the workflow's GitHub OIDC identity. The job verifies the signature before publishing.
5. Publishes with `gh release create --verify-tag --generate-notes`. Tags with a suffix like `-rc.1` are marked as pre-releases.

Assets per release:

```
hello-mondoo_{linux,darwin}_{amd64,arm64}
hello-mondoo_windows_{amd64,arm64}.exe
hello-mondoo.sbom.spdx.json
SHA256SUMS
SHA256SUMS.sigstore.json
```

To cut a release:

```
git tag -a v1.0.0 -m "v1.0.0"
git push origin v1.0.0
```

## Deployment hand-off

Deployment lives in a separate repo, [pe-challenge-p2-container](https://github.com/clickbg/pe-challenge-p2-container), which builds the container image and holds the Kubernetes manifests.

`.github/workflows/dispatch.yml` connects the two. When the Release workflow finishes successfully for a tag push, it:

1. Reads the tag from the finished run and checks it's semver.
2. Mints a GitHub App installation token that lasts one hour, covers only the deploy repo, and is narrowed to `contents: write` (all `repository_dispatch` needs).
3. Sends `repository_dispatch` with `event_type: app-released` and `{tag, source_run}` as the payload.

The deploy repo takes it from there: it verifies this repo's release signature, smoke-tests the image in kind, pushes and signs it, and pins the manifest to the new digest.

Why `workflow_run` and not `on: release`: the release is created with `GITHUB_TOKEN`, and GitHub doesn't start new workflows from events caused by `GITHUB_TOKEN`. A `release: published` trigger would never fire. `workflow_run` is normally treated with care because it runs with secrets after another workflow. Here it never checks out or executes anything from the triggering run, it only reads the tag name.

Why a GitHub App and not a PAT: a PAT is tied to a person and lives for months. The App's private key stays in this repo's secrets and is only ever exchanged for short-lived, repo-scoped tokens.

## Verifying a release

```
TAG=v0.1.0
mkdir -p /tmp/hm && cd /tmp/hm
gh release download "$TAG" -R clickbg/pe-challenge-p1-go-app

cosign verify-blob --bundle SHA256SUMS.sigstore.json \
  --certificate-identity "https://github.com/clickbg/pe-challenge-p1-go-app/.github/workflows/release.yml@refs/tags/$TAG" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  SHA256SUMS

sha256sum -c SHA256SUMS
```

The certificate identity ties the signature to this repo, this workflow file and this tag. A signature made by any other workflow, fork or ref fails verification.

## Local development

`make` with no target lists everything. The targets run the same commands and tool versions as CI, so a failing job can be reproduced locally.

```
make build          # host binary in ./dist
make run            # HTTP_PORT=9090 make run to change the port
make test           # race detector + coverage
make lint           # golangci-lint, same version as CI
make vuln           # govulncheck
make sast           # semgrep in Docker, same image digest and rulesets as CI
make check          # all of the above
make cross          # every release target into ./dist
make clean
```

## Decisions and tradeoffs

**Matrix build instead of GoReleaser.** GoReleaser would do all of this from one config file. I wanted each step (build, SBOM, checksums, signing, publish) to be visible in the workflow and easy to change on its own. For a larger project with more targets, packaging and Homebrew taps, I'd switch to GoReleaser.

**Plain binaries, not archives.** Asset URLs are stable and predictable (`.../download/v1.0.0/hello-mondoo_linux_arm64`), so the Phase 2 Dockerfile can fetch the right one by `TARGETARCH` without unpacking. A single static binary has nothing else to put in an archive.

**One signed checksums file.** Signing `SHA256SUMS` instead of each asset keeps verification to two commands no matter how many targets there are. One `verify-blob` plus `sha256sum -c` covers every file. Keyless signing means there is no private key to store, rotate or leak.

**SBOM from the binaries, not the source tree.** The module has no dependencies, so a source SBOM would be almost empty. Scanning the binaries records the Go toolchain and stdlib version each one was built with, which is what matters for vulnerability matching on a stdlib-only app.

**No build cache in release builds.** CI uses the setup-go module and build cache for speed. Release builds turn it off, because that cache can be written by PR runs, and release binaries shouldn't depend on anything a PR could have put there.

**CI as a reusable gate.** The release calls `ci.yml` instead of re-implementing the checks, so the two can't drift apart.

**Pinned everything.** Actions are pinned to full commit SHAs, and the semgrep container to a digest. Tags can be moved. SHAs and digests can't.

**Dependabot with a 7 day cooldown.** Weekly updates for gomod and GitHub Actions, with action bumps grouped into one PR. The cooldown waits a week before offering a new version. Hijacked releases are usually spotted and pulled within days, so this keeps them out without falling far behind. Security updates skip the cooldown. Dependabot does not track the semgrep image digest, the golangci-lint and govulncheck versions in `ci.yml`, or the `toolchain` line in `go.mod`. Those are bumped by hand. Renovate with regex managers could cover them, but switching tools wasn't worth it here.

**Least privilege.** Default workflow permissions are `contents: read` in CI and none in the release workflow, with each job granted only what it needs. Only the final release job gets `contents: write` and `id-token: write`, and it never checks out code. Checkouts use `persist-credentials: false`. The workflows pass zizmor with the pedantic persona except for one note: zizmor suggests the new `$/` self-repository syntax for the reusable workflow call. A `./` call already resolves to the caller's commit, and I couldn't confirm the new syntax is supported for job-level `uses`, so I kept `./`.

**Concurrency.** PR runs cancel older runs on the same branch. Runs on `main` and release runs never cancel, so every commit on main keeps its result and a release is never stopped halfway.

**semgrep, not CodeQL.** semgrep was required by the brief and covers SAST for code this size. CodeQL would mostly duplicate it, at the cost of a much slower job.

**Coverage is 33%.** `portFromEnv` and the handler are fully tested. `main` and `run` (signal handling, server start) are not. Restructuring them just to raise the number didn't seem worth it. The CI smoke test covers the real startup path, including `HTTP_PORT`.

**Tool versions live in two places.** `ci.yml` and the `Makefile` both pin golangci-lint, govulncheck and the semgrep image, with a comment in each pointing at the other. Having CI call `make` would remove the duplication, but CI would lose golangci-lint-action's caching and inline annotations.

## Repository settings

Not visible in code, but part of the setup:

- Dependency graph, Dependabot alerts, malware alerts and grouped security updates are on.
- Code scanning receives semgrep SARIF from CI.
- `DISPATCH_APP_CLIENT_ID` (variable) and `DISPATCH_APP_PRIVATE_KEY` (secret) for the `clickbg-release-dispatch` GitHub App. The App has Contents read/write only and is installed on the deploy repo only.
- Recommended next: a tag ruleset limiting who can create `v*` tags, since pushing a tag is what publishes a signed release.

## License

BSD 2-Clause, see [LICENSE](LICENSE).
