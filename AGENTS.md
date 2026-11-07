# Contributor and agent instructions

## Language and documentation

- Write developer documentation, code comments, and local memories in concise English.
- Keep runtime errors, logs, generated comments, and code text in English. Literal Cyrillic belongs only in `docs/ru/` and Russian localization resources. Use Unicode escapes for essential Cyrillic test fixtures without changing their runtime values. Run `make check-language` to verify this boundary.
- Explain the purpose, contract, ownership, or important constraint of significant types and functions. Avoid comments that merely repeat their names.
- Preserve translated UI messages, test inputs, protocol examples, and original third-party data when their language is part of behavior or provenance.
- Keep public project documentation in the repository: README.md, AGENTS.md, CONTRIBUTING.md, SECURITY.md, and LICENSE.
- Store internal plans, investigations, work logs, deployment reports, and visual QA reports only in ignored docs-reports/. Use docs-reports/design-qa.md even when a skill suggests a root-level report.
- Keep Serena memories in ignored .serena/. Never force-add local reports, memories, credentials, or production configurations to Git.

## Engineering

- If Serena is available, activate the current project, check onboarding, and read relevant local memories before analysis. Verify memories against source files. Prefer Serena symbol tools; otherwise continue with available tools.
- Keep configuration changes consistent across the Go model, compiler, API, frontend types, examples, and tests.
- Preserve the shared frontend draft/Apply workflow; do not bypass it with separate configuration writes.
- Keep compilation deterministic; put runtime effects in the daemon, engine, and firewall layers.
- Preserve build constraints, generated-file markers, license notices, and compiler/tool directives when editing comments.
- Do not access or modify a live router without an explicit deployment or diagnostic request. Use synthetic configurations in tests.

## Verification

- Run relevant Go tests with the Makefile's sing-box build tags; use CGO_ENABLED=0 where a C toolchain is unavailable.
- Run frontend tests and the production build after frontend changes. Run installer tests when changing installer behavior.
- Use make integration for routing/DNS/firewall changes when Docker is available; it runs privileged network namespaces inside a disposable container.
- For documentation-only edits, check links and prove executable tokens are unchanged rather than adding implementation-mirroring tests.
- Report validation limits honestly. A successful HTTP probe does not prove HTTPS transfers or DNS-leak protection.

## Publication

- The project is licensed under GPL-3.0-only; retain third-party notices and document separate data licenses.
- Use placeholders in examples. Keep local device identifiers, production logs, and credentials out of public documentation.
- Preparing public files does not authorize publishing, pushing, rewriting Git history, or deploying changes.
