import assert from 'node:assert/strict'
import { test } from 'node:test'
import { formatBuild } from '../src/buildIdentity.ts'

test('formats the build variant, architecture, and source revision', () => {
  assert.equal(formatBuild({ variant: 'lite', arch: 'arm64', revision: 'a1b2c3d' }), 'lite · ARM64 · a1b2c3d')
  assert.equal(formatBuild({ variant: 'full', arch: 'mipsle', revision: 'a1b2c3d', dirty: true }), 'full · MIPSLE · a1b2c3d+dirty')
})

test('omits the revision and its separator when the binary has no revision', () => {
  assert.equal(formatBuild({ variant: 'full', arch: 'arm64', dirty: true }), 'full · ARM64')
})
