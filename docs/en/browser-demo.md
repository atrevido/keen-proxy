# Browser demo

<a class="md-button md-button--primary" href="../demo/">Open the interactive demo</a>

The demo is built from this repository's real Vue components, styles, translations, and shared draft store. Only the API implementation and static-host routing change. It is a separate build, not a second interface maintained by hand.

Try editing a group, inspecting a connection, previewing a sample preset, selecting client policies, Apply/Discard, or running simulated diagnostics. Switch English/Russian and the theme from the toolbar.

- Configuration is kept in this page's memory and resets on reload.
- The demo does not contact a router or proxy server, query DNS, fetch filter lists, or perform connectivity checks.
- Never enter actual credentials. Use the supplied placeholders.
- The monitor simulates traffic every three seconds. The routing journal receives new synthetic events and supports live pause, filters, and pagination. Apply adds an audit entry. The catalog contains one synthetic preset.
- **Overview** shows simulated CPU and memory for the router and the KeenProxy process. In **Settings → Updates**, try installing the sample release, rolling back, or saving an automatic update time. Choose the next minute on the displayed UTC clock to see a scheduled installation while the section is open. The demo does not download a release or restart a service.
- **Diagnostics → Download report** exports synthetic check results. To collect retained journals and applied settings as a ZIP, use the [Diagnostics archive](troubleshooting.md#diagnostics-archive) on the router.

- YAML import is unavailable. Synthetic exports are JSON, which is compatible with YAML 1.2, rather than a production backup format.

The production build continues using the authenticated HTTP API. [Build instructions](development.md) explain how both outputs are created.
