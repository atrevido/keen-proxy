import assert from 'node:assert/strict'
import { test } from 'node:test'
import { deviceDisplayName, deviceMatches } from '../src/deviceNames.ts'

test('configured name wins without replacing the discovered hostname', () => {
  const device = { mac: 'aa:bb:cc:dd:ee:01', ip: '192.0.2.10', hostname: 'Printer.local', hostname_source: 'dhcp' as const }
  assert.equal(deviceDisplayName({ name: 'Office' }, device, device.ip, device.mac), 'Office')
  assert.equal(deviceDisplayName(undefined, device, device.ip, device.mac), 'Printer.local')
  assert.equal(deviceDisplayName({ name: '' }, device, device.ip, device.mac), 'Printer.local')
  assert.ok(deviceMatches(' PRINTER ', { name: 'Office' }, device, device.ip, device.mac))
  assert.ok(deviceMatches('office', { name: 'Office' }, device, device.ip, device.mac))
  assert.ok(deviceMatches('192.0.2', undefined, device, device.ip, device.mac))
  assert.ok(deviceMatches('AA:BB', undefined, device, device.ip, device.mac))
  assert.equal(deviceMatches('missing', undefined, device, device.ip, device.mac), false)
})

test('unnamed devices fall back to their address and never mutate configuration', () => {
  const client = { name: '' }
  assert.equal(deviceDisplayName(client, undefined, '192.0.2.10', 'mac'), '192.0.2.10')
  assert.equal(deviceDisplayName(undefined, undefined, '', 'mac'), 'mac')
  assert.equal(client.name, '')
  assert.equal(deviceMatches('missing', undefined, undefined, '192.0.2.10', 'mac'), false)
})
