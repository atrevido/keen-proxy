# Security and sensitive reports

KeenProxy changes router DNS, firewall, and traffic routing. It is experimental, and the repository does not yet promise a supported-version window or security response time.

## Reporting a vulnerability

Do not include exploitable details or secrets in a public issue. Use GitHub private vulnerability reporting if enabled for the repository. If no private reporting channel is published, open a minimal issue asking the maintainer for a private channel without disclosing the vulnerability or credentials. No unverified contact address is implied by this document.

A useful private report includes the affected commit, build tags, router architecture/firmware, minimal synthetic reproduction, impact, and any suggested fix. Share only the data necessary to reproduce the issue.

## Protect configuration and access

- Treat configuration GET/export and raw connection profiles as sensitive even after authentication.
- Prefer the [diagnostic ZIP](docs/en/troubleshooting.md#download-the-diagnostic-zip) for support. Follow the guide's privacy checklist before sharing any export.
- Keep the management API on a trusted private network. Trusted-network access without a token is an explicit administrator policy, not an authentication mechanism for untrusted clients.
- Preserve SSH access and a recovery path before changing interception or DNS settings.
- Diagnose individual HTTP, HTTPS, data-transfer, and DNS paths separately. A green health badge is not a general security or connectivity guarantee.

Publishing the current source tree does not remove sensitive material from earlier Git commits. Review repository history and any release artifacts before making a repository public.
