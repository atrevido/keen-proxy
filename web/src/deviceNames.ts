import type { Client, Device } from './types'

// Discovered labels are presentation hints; configured client names retain priority.
export function deviceDisplayName(client: Pick<Client, 'name'> | undefined, device: Device | undefined, ip: string, mac: string): string {
  return client?.name || device?.hostname || ip || mac
}

// Search includes discovered names even when a manual name overrides their display.
export function deviceMatches(query: string, client: Pick<Client, 'name'> | undefined, device: Device | undefined, ip: string, mac: string): boolean {
  return [client?.name, device?.hostname, ip, mac].some(value => value?.toLowerCase().includes(query.trim().toLowerCase()))
}
