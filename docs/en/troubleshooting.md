# Troubleshooting

## The demo says a tunnel works. Is it real?

No. Every address, counter, DNS result, and connectivity check in the browser demo is simulated. It does not contact the listed VPN servers, resolve your domains, download lists, or configure a router. Reload the page to reset configuration changes.

## I edited a profile, but traffic did not change

Look for pending changes and choose **Apply**. All screens share a draft. Confirm that the selected network/client uses the intended routing mode, then inspect the route journal and test the applied connection.

## HTTP passes but the application fails

Run HTTPS and transfer checks separately. Inspect certificate/time errors, profile details, system logs, and fallback behavior. A process being alive or one HTTP response succeeding is weaker evidence than a successful application transfer.

## A domain has no current IPs

Check whether the group is enabled and applied. Review its DNS route, resolver availability, expiration and error details. Manual preview observations are separate from applying the configuration. Demo `.invalid` names intentionally return synthetic errors.

## What should a bug report include?

Expected and actual behavior, exact version/build tags, architecture/firmware, reproducible steps, and a minimal synthetic configuration. Remove tokens, connection URLs, private keys, personal browsing histories, and production exports. Read [CONTRIBUTING](https://github.com/atrevido/keen-proxy/blob/main/CONTRIBUTING.md) and [SECURITY](https://github.com/atrevido/keen-proxy/blob/main/SECURITY.md).

## What is still a documentation draft?

This is an initial guide. Firmware-specific startup/recovery procedures and a tested-router compatibility matrix still require reproducible device evidence. They are not inferred from the demo or from another project's installation guide.
