# Troubleshooting

## The demo says a tunnel works. Is it real?

No. Every address, counter, DNS result, and connectivity check in the browser demo is simulated. It does not contact the listed VPN servers, resolve your domains, download lists, or configure a router. Reload the page to reset configuration changes.

## I edited a profile, but traffic did not change

Look for pending changes and choose **Apply**. All screens share a draft. Confirm that the selected network/client uses the intended routing mode, then inspect the route journal and test the applied connection.

## HTTP passes but the application fails

Run HTTPS and transfer checks separately. Inspect certificate/time errors, profile details, system logs, and fallback behavior. A process being alive or one HTTP response succeeding is weaker evidence than a successful application transfer.

## A domain has no current IPs

Check whether the group is enabled and applied. Review its DNS route, resolver availability, expiration and error details. Manual preview observations are separate from applying the configuration. Demo `.invalid` names intentionally return synthetic errors.

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
