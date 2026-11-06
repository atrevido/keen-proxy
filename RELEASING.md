# Preparing a public release

This guide is for maintainers preparing an initial public repository or a later source/binary release. It does not imply that a release has been published or that every router is supported.

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
