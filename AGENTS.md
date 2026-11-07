# Contributor and agent instructions

## Language and documentation

- Whenever a new feature or behavior change requires documentation, add or update the affected documentation and user guides in the same change. Keep the English and Russian versions in sync.
- Write developer documentation, code comments, and shared Serena memories in concise English.
- Keep runtime errors, logs, generated comments, and code text in English. Literal Cyrillic belongs only in `docs/ru/` and Russian localization resources. Use Unicode escapes for essential Cyrillic test fixtures without changing their runtime values. Run `make check-language` to verify this boundary.
- Explain the purpose, contract, ownership, or important constraint of significant types and functions. Avoid comments that merely repeat their names.
- Preserve translated UI messages, test inputs, protocol examples, and original third-party data when their language is part of behavior or provenance.
- Keep public project documentation in the repository: README.md, AGENTS.md, CONTRIBUTING.md, SECURITY.md, and LICENSE.
- Store internal plans, investigations, work logs, deployment reports, and visual QA reports only in ignored docs-reports/. Use docs-reports/design-qa.md even when a skill suggests a root-level report.
- Treat everything in [`.serena/memories/`](.serena/memories/core.md) as public. Store only durable project knowledge there. Keep credentials, private network addresses, device identifiers, production configuration, private transcripts, and deployment records out of shared memories. Keep local Serena configuration and caches ignored.

## Required workflow for AI agents

1. Load [Superpowers](https://github.com/obra/superpowers) through the agent host and follow `using-superpowers` plus the skills relevant to the task. Use brainstorming and planning for features, systematic debugging for failures, test-driven development for behavior changes, and verification before completion. Follow the repository's authorization and workspace rules throughout.
2. Apply [ub-mode](https://github.com/unclebob/ub-mode) whenever you write, change, refactor, or review code. Read its `SKILL.md` before editing. Work in the order **First Make it Work → Then Make it Right → Follow Practice**. Make the smallest change that solves the problem, test behavior at its normal entry point, and keep a short verification log in `docs-reports/`.
3. Before analyzing code, read [`core`](.serena/memories/core.md) and the relevant project memories. When [Serena](https://github.com/oraios/serena) is available, activate this checkout, check onboarding, and prefer Serena for memory reads, symbol lookup, and reference analysis. If Serena is unavailable, open `.serena/memories/core.md` directly and follow relevant `mem:<name>` references by reading `.serena/memories/<name>.md` with the available file tools. In either case, verify memory claims against the current source.

Install the skills and Serena using their upstream instructions for your agent host. Install the official [`crapper`](https://github.com/unclebob/crapper), [`dryer`](https://github.com/unclebob/dryer), and [`mutator`](https://github.com/unclebob/mutator) launchers outside the repository. Follow their upstream setup, including keeping `mutator` next to `crapper`, and make the commands available on `PATH`. Use isolated tool environments. Check `--help` before the first run.

For changed code, run all three checks on the affected source files and use the project's existing test commands:

- **Crapper:** Use real Go `-coverprofile` output with the Makefile tags or real LCOV from the frontend tests. Aim for CRAP below 10 for changed functions. Report gaps in coverage and any existing complexity that remains.
- **Dryer:** Run `dryer --threshold 0.8 <affected files>` and inspect the duplicate candidates.
- **Mutator:** Run `mutator --test-command '<project test command>' <affected files>` in its isolated workers. Use a bounded file or line selection. Report executed, killed, survived, and uncovered sites. A scan, a failed baseline, or a run that selects zero mutations does not verify mutation resistance.

If a skill, Serena, or a quality tool is unavailable, report the specific limitation and run the applicable repository checks. If an analyzer cannot read a format such as a Vue SFC, verify the behavior through the normal tests and build and report the analyzer gap. Use [the verification memory](.serena/memories/task_completion.md) for project test entry points.

## Engineering

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
