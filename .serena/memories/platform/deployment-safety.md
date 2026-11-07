# Router deployment safety
- Use a live router only for explicitly authorized work. Inspect the active service and the interception state before changes; old reports are not evidence of current state.
- Validate the target architecture and candidate configuration first. Preserve the existing configuration and a verified rollback binary. Keep token files, VPN profiles, hostnames, and logs out of shared memories and reports intended for publication.
- Avoid running competing interception processes. Check the panel, DNS over UDP and TCP, and actual firewall counters after restart. A successful connection probe does not prove that sustained traffic works.
- Record per-device deployment state, backup locations, and operational evidence only in ignored docs-reports/.
