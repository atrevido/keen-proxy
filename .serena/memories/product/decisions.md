# Product decisions
- Focus on Keenetic and Netcraze with Entware. Use embedded sing-box and a compiled Vue interface. Keep the visual design original and maintain both English and Russian UI.
- Keep country groups distinct from domain and IP entries. Explicit group rules precede country rules within routed policies; global block rules run first. Invert the union of excluded countries.
- Prefer readable TLS SNI and HTTP Host. Apply DNS-derived IP rules only when no readable hostname exists. Resolve active preset domains after Apply.
- Keep DNS routes and direct fallback explicit. Encrypted DNS alone does not establish tunnel routing or prove leak prevention.
- Failover follows an ordered chain with hysteresis. Allow direct fallback only when explicitly configured.
- Keep one YAML configuration and one shared draft. Preview, imports, connection wizards, and bulk edits must leave live settings unchanged until Apply.
- Make routing decisions explainable in the journal, with filters, live updates, and masked export. Keep secrets concealed in connection editing and omitted from diagnostics.
