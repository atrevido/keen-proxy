# Contributing to KeenProxy

KeenProxy is being prepared for an international contributor community. Useful contributions include router compatibility reports, reproducible routing tests, protocol integration, accessibility, translations, and clearer documentation.

## Start with a reproducible case

Describe the expected behavior, actual behavior, commit/build tags, router architecture, firmware, and relevant kernel capabilities. Include a minimal synthetic configuration and the exact command or UI action that triggers the issue. Keep one issue or change focused on one problem.

Never attach real private keys, passwords, tokens, connection URLs, complete production YAML, unreviewed support exports, or browsing histories. A hostname or process being reachable does not prove that a tunnel transfers application data successfully.

For a router-specific report, explain how you retained access and restored the previous state. Do not ask maintainers to run unknown scripts against a live router. Use the local demo and disposable integration suite whenever possible.

## Development setup

See the root [README](README.md) for toolchain requirements and build commands. Go build tags are significant: an untagged test run does not exercise the normal protocol registry. Frontend dependencies are locked with npm; use `npm ci`.

The main code boundaries are:

| Area | Responsibility |
| --- | --- |
| `internal/model` | Configuration shape, defaults, parsing, and validation |
| `internal/compile` | Deterministic sing-box/rule-set/firewall-plan generation |
| `internal/daemon` | Runtime lifecycle, applying changes, recovery, and health |
| `internal/netfilter` | Owned iptables chains and policy-routing lifecycle |
| `internal/dnsfront`, `domaincache`, `filterlist` | DNS transport, bounded observations, and DNS-only filtering |
| `internal/engine`, `links`, `trusttunnel` | Embedded engine and protocol/client adaptation |
| `internal/api`, `journal`, `stats` | Authenticated management, journals, and telemetry |
| `web/src` | Typed API client, shared draft state, and Vue interface |

## Make the smallest verifiable change

- Follow Go formatting and the existing TypeScript/Vue style.
- Write short English comments explaining contracts, ownership, units, and non-obvious ordering. Keep comments close to the code they describe.
- Preserve configuration compatibility, the shared draft/Apply workflow, and rollback behavior.
- Update the Go model, compiler, API, frontend types, examples, and tests together when changing the schema.
- Add a failing behavioral test before fixing a bug when practical. Use controlled clocks and injected network boundaries instead of real services.
- Keep compiler directives, generated-file markers, and third-party notices intact.
- Preserve meaningful Unicode and localized fixtures. Translate UI messages through the locale dictionaries.

## Check the change

Run the relevant tests while developing, then verify the final change:

```sh
CGO_ENABLED=0 make test
CGO_ENABLED=0 make vet
cd web && npm test && npm run build
```

For routing, DNS, or firewall behavior, also run `make integration` with Docker. This uses a privileged disposable container; review the Makefile and test script before running it. Changes to separate tools should be tested from their own Go modules as appropriate.

For a comment-only change, executable-token comparison plus normal build/test checks can establish that behavior was preserved. Do not invent tests that only assert comment wording. Report unavailable checks and remaining failures explicitly.

## Submit a reviewable change

Explain the concrete problem and resulting behavior. Include relevant validation, compatibility considerations, and any untested router-specific assumptions. Keep unrelated formatting or refactors out of the diff.

## Versioning and pull request titles

KeenProxy uses [Semantic Versioning](https://semver.org/): `MAJOR.MINOR.PATCH`. Choose the bump by the effect on users, including configuration files, API, CLI, installation, and upgrades.

| Change | Bump after 1.0.0 | Example |
| --- | --- | --- |
| Backward-compatible bug fix | Patch | `1.4.2` to `1.4.3` |
| Backward-compatible feature | Minor | `1.4.2` to `1.5.0` |
| Incompatible change requiring users or clients to adapt | Major | `1.4.2` to `2.0.0` |

Increasing a component resets the components to its right to zero. A release uses the highest bump required by its accumulated changes; commits do not each require a public release. Internal refactoring alone does not require a major bump. Document incompatible changes and migration steps even when they fix a bug or a security issue.

Use [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) for PR titles and the resulting squash commit:

- `fix: preserve settings after restart` requests a patch release.
- `feat: add a proxy protocol` requests a minor release.
- `feat!: replace the configuration format` marks a breaking change. Explain the migration in the PR description and preserve a `BREAKING CHANGE:` footer in the squash commit when used.
- `docs:`, `test:`, `ci:`, `chore:`, and internal `refactor:` changes do not request a release by themselves unless marked as breaking. Classify user-visible fixes and features by their actual effect.

Before `1.0.0`, the project uses patch bumps for compatible fixes and minor bumps for features or incompatible changes. Keep breaking-change markers and migration notes during this phase. This is a project convention: SemVer itself does not promise a stable public API for `0.x`. Release automation must be configured to match this policy.

Version `1.0.0` marks a defined, stable user-facing contract and a commitment to preserving compatibility; it does not mean the software has no bugs. Release candidates use versions such as `1.5.0-rc.1`, followed by `1.5.0-rc.2` or the final `1.5.0`.

Maintainers review the proposed version and changelog in a release PR before publishing. See [RELEASING.md](RELEASING.md) for the release process and automation status.

Public project guides live in the repository. Local plans, investigations, generated QA reports, and deployment notes belong in ignored `docs-reports/`; local agent memory belongs in ignored `.serena/`. Do not force-add those directories or production artifacts.

By contributing, you agree that your original contributions are provided under the project's GPL-3.0-only license. Retain existing copyright/license notices and clearly identify any third-party code or data. Do not contribute material you are not authorized to redistribute.
