# Panel and API
- web/src/store.ts owns the reactive draft and saved snapshot. Apply PUTs the complete /api/config. Discard resets the draft and failed Apply leaves it available for correction.
- The panel uses Vue, Naive UI, and a shared API client. Keep same-origin auth, the shared 401 handler, and EventSource for journal updates. Unknown /api/ routes return 404 rather than the SPA.
- Keep both demo paths in sync with the API. Go api.Demo serves synthetic data through the real HTTP layer. Vite demo mode aliases web/src/api.ts imports to web/src/demo/api.ts and uses relative assets for static hosting. Demo configuration is in memory.
- API access checks RemoteAddr. Do not trust forwarded headers. Credentials use bearer auth or the session cookie; configured no-token CIDRs come from applied configuration.
- Keep EN messages typed against RU. Preserve browser locale, the stored user preference, and the Naive UI locale. Use translation keys for route titles.
- ConnTabs uses ResizeObserver for overflow and exposes hidden connections through a dropdown. Keep toolbar actions outside the scrolling tab area.
- Groups expand to domain/IP/TTL rows. Preview resolution is isolated from active routing. Match frontend domain normalization against model.ParseEntry.
- Health status measures HTTP reachability and freshness. Keep the reactive clock scoped to the component lifecycle. Diagnostics distinguishes HTTP, HTTPS, and transfer checks.
- LAN names are discovered in the background. Show the manual name first, then the DHCP or local DNS name, then address identifiers. Search both names. Discovery does not edit the configuration.
