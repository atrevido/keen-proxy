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

A clean exit removes the managed hook. Uninstall also cleans up the managed hook after the service has stopped, including leftovers from an interrupted shutdown. Foreign hook files are preserved. A callback queued after shutdown or binary rollback exits harmlessly when the event file is absent. The system journal records `netfilter event hook enabled` at startup. Each repair attempt appears once in the audit journal with its check trigger: **Netfilter hook** or **Watchdog timer**. The panel shows a short result and rule identity; expand the entry for exit codes and the original command errors. Historical entries without trigger metadata show **Not recorded**. Check-and-recovery time measures the work from the first check through verification, not outage duration. Successful recovery and routine event checks are not repeated in the system journal; hook setup/watcher errors and failed recovery remain there. Firmware callback queues and rule application still take time: event recovery does not guarantee zero packet loss. See the [NDM hook contract](https://support.keenetic.ua/titan/kn-1811/en/42407-opkg-component-description.html).

After applying the rules, the watchdog checks the entry jumps again. If verification fails, it waits briefly and retries the repair once to allow for firmware replacing several tables in succession. The counter increases only after verified recovery. A failure records the last outcome and any available checks. If both a configuration apply and its rollback fail while installing firewall rules, the watchdog can retry the pending plan once its engine, DNS listener, and policy routing are ready. A successful check establishes entry-jump presence at that moment. It does not validate every rule inside the chains, TPROXY policy routes, or end-to-end connectivity.

For repeated recoveries, record the times of the triggering actions and export the diagnostics ZIP together with the router system log. The ZIP retains structured firewall evidence under the audit entry's `firewall` field, including the optional `trigger` (`netfilter_hook` or `watchdog`) and nonnegative `duration_ms`, any failed `before` and `after` checks, and `outcome` (`restored`, `apply_failed`, or `verify_failed`). Raw command errors remain in the local journals. The ZIP keeps only known rule identities, exit codes, reasons and coarse error categories. Older entries have no `firewall` field.

## Download the diagnostic ZIP

1. Reproduce the problem and note the approximate time and time zone. Open **Diagnostics** in the web panel on your router.
2. Find **Diagnostics archive** near the top of the page, below **Build details** when shown. Choose **Download ZIP**. Open the downloaded `keenproxy-diagnostics.zip` and review its contents before sharing.

| File | What it contains |
| --- | --- |
| `README.txt` | Archive format and a summary of the privacy rules |
| `config.json` | A sanitized summary of applied settings, including routing, connections, and DNS policies |
| `status.json` | Runtime state at export time, including build identity, interception, connection health, and active fallbacks |
| `journals/routes.jsonl`, `journals/audit.jsonl`, `journals/system.jsonl` | All readable records still retained by KeenProxy, with one event per line |

The ZIP includes records still in memory and on disk, regardless of the filters, grouping, or time range selected in **Logs**. Older records may have rotated out. Draft changes are outside the archive.

The export omits credentials, raw connection profiles, and free-form log messages. Names and addresses are removed or replaced with aliases that are consistent within one ZIP. Event times, ports, protocols, policies, and counts can still identify activity. Review the files before attaching them to an issue or sharing them privately.

For one journal as plain text on the router, open **Logs**, select **Routing**, **Audit**, or **System**, then choose **Download log** and a time range. The text export masks known secrets but can include readable domains, addresses, and log details. Use the ZIP for a support report covering all three journals.

**Download report** on the Diagnostics screen saves `keenproxy-diagnostics.json` with a status summary and results from any checks you ran for the selected connection. In the browser demo this report contains synthetic data, and ZIP download is available only in the router panel. For a restorable configuration, use **Settings → YAML configuration** and store the export privately.

## What should a bug report include?

Include the expected and actual behavior, the running release and build details, router model and firmware, steps to reproduce, and the approximate time of the incident. Use a minimal synthetic configuration or attach a reviewed [diagnostic ZIP](#download-the-diagnostic-zip). Read [CONTRIBUTING](https://github.com/ordo-systems/keen-proxy/blob/main/CONTRIBUTING.md) and [SECURITY](https://github.com/ordo-systems/keen-proxy/blob/main/SECURITY.md).
