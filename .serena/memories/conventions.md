# Conventions
- Internal Go packages; model fields use matching yaml/json names, Defaults + Validate, and aggregate configuration errors. Validate before Compile (which contains mustPort).
- model is the source of truth. Schema changes require matching web/src/types.ts, examples, demo API, compiler, and tests.
- compile is a pure deterministic transformation; runtime effects belong in daemon/netfilter/engine.
- Use English for source, runtime messages, and shared memories. Preserve translations in the localization resources. Use gofmt, slog, %w error wrapping, standard testing, table cases, and dependency interfaces.
- Vue SFC with TypeScript; shared draft store without Pinia. All screens edit one configuration; Apply submits it whole. Do not bypass this with separate saves.
- UI RU/EN in web/src/locales; use computed translated option lists and t from i18n.ts outside components.
- Follow the Superpowers, ub-mode, and quality-tool workflow in AGENTS.md. Preserve existing uncommitted work.
