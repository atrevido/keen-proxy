# Hardware verification boundaries
- Check LAN DNS over both UDP and TCP, plus a request aimed at an external resolver when interception is configured. Confirm packet counters on the intended chains and check a negative answer.
- Test HTTP reachability, certificate-verified HTTPS, and a bounded transfer separately for each relevant connection. Compare a reproducible transport failure against a known baseline before attributing it to a build.
- If DHCP hostname lookup is available, check that discovered labels appear without overriding manual names or mutating configuration. Absence of a discovered name is a valid result.
- A relay can help isolate a direct-path transport failure. It cannot by itself identify who caused the failure. Keep environment-specific findings in private reports.
