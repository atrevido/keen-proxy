# Change verification
- Run make check-language for English source and shared memory changes. Check public document links and memory references. Keep local evidence in docs-reports/.
- Run CGO_ENABLED=0 make test and CGO_ENABLED=0 make vet for relevant Go changes. Use the Makefile protocol tags in focused tests and coverage runs.
- For frontend changes run npm --prefix web test and npm --prefix web run build. Also run npm --prefix web run build:demo when demo contracts change.
- Run make integration for DNS, firewall, and routing changes. Verify Linux packet behavior in disposable namespaces before requesting hardware validation.
- Apply the required Crapper, Dryer, and Mutator workflow in AGENTS.md to changed code. Keep scopes small and report unsupported files, missing coverage, and mutation survivors.
- Inspect the final diff, confirm only intended files are visible to Git, and distinguish unit, integration, demo, and hardware evidence.
