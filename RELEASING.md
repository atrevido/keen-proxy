# Preparing a public release

This guide is for maintainers preparing an initial public repository or a later source/binary release. It does not imply that a release has been published or that every router is supported.

## Version and release pull request

Follow the versioning and Conventional Commit rules in [CONTRIBUTING.md](CONTRIBUTING.md#versioning-and-pull-request-titles). The selected workflow uses [Release Please](https://github.com/googleapis/release-please) to propose releases, with a maintainer reviewing each release PR.

The repository defines [CI](.github/workflows/ci.yml) and [release](.github/workflows/release.yml) workflows. CI runs on pull requests and can be started manually. Pushes to `main` run the same checks before Release Please. They become active after the workflow files are pushed to GitHub and Actions is enabled.

1. Merge reviewed changes into `main` after the relevant checks pass. With squash merges, preserve the Conventional Commit PR title and any breaking-change details in the resulting commit.
2. Release Please accumulates releasable changes since the previous release and creates or updates a release PR containing the proposed version and changelog. Its pre-1.0 settings match the project's policy.
3. A maintainer checks the bump against the actual compatibility impact, edits release notes as needed, and verifies migration instructions, validation results, and the publication checks below. The automation's proposal does not replace this review.
4. Merge the release PR when the release is ready. After checks pass on `main`, Release Please creates the corresponding Git tag, such as `v1.5.0`, and GitHub Release. A dependent job builds from that exact tag and attaches full/lite ARM64/MIPSLE binaries, `install.sh`, the Entware service script `S99keenproxy`, source archive, license notices, build identity, and `SHA256SUMS`. Check that the asset job succeeds before announcing the release.
5. The default automation produces stable releases. Release candidates require a separately reviewed prerelease configuration and must be marked as prereleases with their known limits. A candidate such as `v1.5.0-rc.1` must pass the relevant release checks before promotion to `v1.5.0`.

Use the Git tag as the release identity and derive build versions from it; any tracked version metadata must be updated consistently by the release PR. Keep published versions immutable: do not move a published tag or replace its binaries with a different build. Publish a new version to correct a released defect.

### GitHub setup and recovery

- Register a private [GitHub App](https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/registering-a-github-app) owned by the repository's organization. Disable its webhook, grant repository write access to Contents, Issues, and Pull requests, and install it only on this repository.
- Create an environment named `release` and allow deployments only from the `main` branch. Store the App's client ID as the environment variable `RELEASE_APP_CLIENT_ID` and its private key as the environment secret `RELEASE_APP_PRIVATE_KEY`. See [GitHub App authentication in Actions](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/making-authenticated-api-requests-with-a-github-app-in-a-github-actions-workflow).
- The version job issues a short-lived token restricted to this repository. The built-in `GITHUB_TOKEN` keeps read permissions by default. The separate asset job requests Contents write to attach files to the release. The app needs access to the Release Please branch and release tags under the repository rules. Keep required reviews and CI in place for `main`.
- After merging a change to release authentication, verify a fresh Release run on `main`. Rerunning an older run uses its original workflow revision. For asset recovery with a supplied tag, the workflow skips the App token step.
- `version.txt` and `.release-please-manifest.json` start at the development baseline `0.1.0`; this does not assert that `v0.1.0` has been published. The initial commit is the bootstrap boundary. The first releasable changes determine the next version; if the first public release must be `0.1.0`, use a reviewed commit with a `Release-As: 0.1.0` footer before merging its release PR.
- Release PRs update `version.txt`, the manifest, `CHANGELOG.md`, and the private frontend package/lockfile version fields together. `BUILD_INFO.txt` records the tag, source commit, toolchains, and protocol build tags; the application does not currently expose a runtime version command.
- The main-branch `tools/bootstrap.sh` reads `version.txt` unless the user supplies `--version`. It needs the selected release's checksum file, binary, offline installer, and init script before touching a running service. Validate these assets on the published release before advertising the installation command.
- If asset publication fails after release creation, manually run the Release workflow from `main` with its existing tag in the `tag` input. Existing assets must be byte-identical before missing ones are uploaded. A different rebuild fails rather than replacing files; investigate toolchain/dependency differences and publish a new version when needed. Running without a tag retries Release Please.
- Configure branch protection or a ruleset separately to require review and passing CI. Workflow files alone do not enforce merge requirements.

## Review the material being published

1. Choose a specific, reviewed commit. Inspect the full tracked tree and every intended release artifact.
2. Check for credentials, production configuration, private device identifiers, personal paths, logs, source maps, and embedded data. Pattern scans are useful but do not replace manual review.
3. Review Git history as well as the current checkout. Ignoring or deleting a file does not remove old versions. If a private development history contains local reports or memories, prepare a separately reviewed public history before publishing; do not push that history blindly or rewrite an existing shared history without coordination.
4. Confirm that public guides use synthetic examples and link only to files included in the public tree. Keep internal work notes and agent memories excluded.

## Check licenses and provenance

- Include the canonical `LICENSE` and applicable third-party notices with source distributions.
- Review the exact pinned versions of Go modules, frontend packages, and optional external executables. Preserve required notices in binary distributions as well.
- Meet GPLv3's corresponding-source requirements when conveying binaries; the corresponding source must match the distributed build and include the necessary build material.
- Confirm permission/license and attribution for community presets and fetched data. A source URL or a community origin does not alone establish redistribution terms.
- Retain DB-IP attribution where its data is displayed. Alternative country databases and blocklists need their own review.
- Preserve upstream naming/association notices. Do not imply endorsement by Keenetic, Netcraze, SagerNet, or another upstream project.

## Validate the selected build

```sh
CGO_ENABLED=0 make test
CGO_ENABLED=0 make vet
cd web && npm ci && npm test && npm run build
```

Run `make integration` for the full network test suite. Test ARM64/MIPS builds and the relevant protocol registry on the intended hardware; a host build alone is insufficient. Auxiliary tools have separate Go modules. Document checks that could not be run.

Build from the reviewed source with dependencies locked, record commit/build tags/toolchains, and generate checksums for the exact published files. Exercise the installer, authentication, configuration backup/recovery, and expected startup mechanism on a disposable or recoverable target.

## Publish only after review

Describe supported behavior, known limits, tested hardware, upgrade/recovery steps, and any incompatible changes. Avoid transferring local operational reports into release notes. Publish a private security-reporting channel before soliciting vulnerability reports where possible.

Creating these files or running local checks is preparation only. Publishing a repository, pushing a branch, changing its visibility, or deploying to a router requires a separate maintainer decision.
