# Troubleshooting

## The demo says a tunnel works. Is it real?

No. Every address, counter, DNS result, and connectivity check in the browser demo is simulated. It does not contact the listed VPN servers, resolve your domains, download lists, or configure a router. Reload the page to reset configuration changes.

## I edited a profile, but traffic did not change

Look for pending changes and choose **Apply**. All screens share a draft. Confirm that the selected network/client uses the intended routing mode, then inspect the route journal and test the applied connection.

## HTTP passes but the application fails

Run HTTPS and transfer checks separately. Inspect certificate/time errors, profile details, system logs, and fallback behavior. A process being alive or one HTTP response succeeding is weaker evidence than a successful application transfer.

## A domain has no current IPs

Check whether the group is enabled and applied. Review its DNS route, resolver availability, expiration and error details. Manual preview observations are separate from applying the configuration. Demo `.invalid` names intentionally return synthetic errors.

## Why does the watchdog restore interception rules?

The watchdog checks the required firewall entry jumps every 30 seconds by default. A failed check triggers a repair attempt. The audit event records the first failed table, built-in chain, target, exit code and command error. Exit code 1 is reported as a failed check, because the code alone cannot establish why the command failed. A value of -1 means no normal process exit code was available.

On a managed Linux installation, KeenProxy also enables an optional NDM callback when `/var/run/ndm.core.socket` and `/opt/etc/ndm` are present. This covers compatible Keenetic/Netcraze firmware without assuming every router supports NDM. The daemon owns `/opt/etc/ndm/netfilter.d/99-keenproxy.sh` and a private runtime event file. IPv4 `nat`, `filter` and `mangle` callbacks request a serialized check of the current rules without restarting the engine. Repeated notifications coalesce. The 30-second watchdog remains active on every platform, including when callback setup fails.

The hook is removed on a clean exit; existing foreign hook files are preserved. A callback queued after shutdown or binary rollback exits harmlessly when the event file is absent. The system journal records `netfilter event hook enabled` at startup. Each repair attempt appears once in the audit journal with its check trigger: **Netfilter hook** or **Watchdog timer**. The panel shows a short result and rule identity; expand the entry for exit codes and the original command errors. Historical entries without trigger metadata show **Not recorded**. Check-and-recovery time measures the work from the first check through verification, not outage duration. Successful recovery and routine event checks are not repeated in the system journal; hook setup/watcher errors and failed recovery remain there. Firmware callback queues and rule application still take time: event recovery does not guarantee zero packet loss. See the [NDM hook contract](https://support.keenetic.ua/titan/kn-1811/en/42407-opkg-component-description.html).

After applying the rules, the watchdog checks the entry jumps again. The restoration counter increases only when this verification passes. Failed application or verification produces an “Interception rules not restored” event with the original check and the subsequent error. A successful check establishes entry-jump presence at that moment. It does not validate every rule inside the chains, TPROXY policy routes, or end-to-end connectivity.

For repeated recoveries, record the times of the triggering actions and export the diagnostics ZIP together with the router system log. The ZIP retains structured firewall evidence under the audit entry's `firewall` field, including the optional `trigger` (`netfilter_hook` or `watchdog`) and nonnegative `duration_ms`, `before`, an optional failed `after` check, and `outcome` (`restored`, `apply_failed`, or `verify_failed`). Raw command errors remain in the local journals. The ZIP keeps only known rule identities, exit codes, reasons and coarse error categories. Older entries have no `firewall` field.

## What should a bug report include?

Expected and actual behavior, exact version/build tags, architecture/firmware, reproducible steps, and a minimal synthetic configuration. Remove tokens, connection URLs, private keys, personal browsing histories, and production exports. Read [CONTRIBUTING](https://github.com/ordo-systems/keen-proxy/blob/main/CONTRIBUTING.md) and [SECURITY](https://github.com/ordo-systems/keen-proxy/blob/main/SECURITY.md).

## What is still a documentation draft?

This is an initial guide. Firmware-specific startup/recovery procedures and a tested-router compatibility matrix still require reproducible device evidence. They are not inferred from the demo or from another project's installation guide.
