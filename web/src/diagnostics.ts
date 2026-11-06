import type { Connection, Status, DiagnosticReport } from './types'
// Only unchanged applied profiles are eligible for engine connectivity checks.
export function sameAppliedConnection(
  draft: Connection | undefined,
  applied: Connection | undefined,
): boolean {
  return (
    !!draft && !!applied && JSON.stringify(draft) === JSON.stringify(applied)
  )
}
// Allowlist of machine results only: never copy display strings or raw errors
// from config, GeoIP download URLs, transport errors or source profiles.
export function safeDiagnosticsReport(
  status: Status | null,
  report: DiagnosticReport | null,
  connection: string,
  dirty: boolean,
  demo: boolean,
) {
  return {
    generated: new Date().toISOString(),
    demo,
    connection,
    draft_changes: dirty,
    status: status
      ? {
          mode: status.mode,
          firewall: status.firewall,
          geoip_loaded: status.geoip.loaded,
          restored: status.restored,
        }
      : null,
    dns_route_verified: false,
    checks: (report?.results ?? []).map((r) => ({
      kind: r.kind,
      ok: r.ok,
      checked: r.checked,
      latency_ms: r.latency_ms,
      bytes: r.bytes,
      error: r.ok ? undefined : 'check_failed',
    })),
  }
}
