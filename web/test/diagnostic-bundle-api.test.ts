import assert from 'node:assert/strict'
import { test } from 'node:test'
import { api, setUnauthorizedHandler } from '../src/api.ts'
import { api as demoAPI } from '../src/demo/api.ts'

test('downloads the ZIP with same-origin session credentials', async (t) => {
  const zip = new Uint8Array([80, 75, 3, 4, 0, 255])
  const fetch = t.mock.method(globalThis, 'fetch', async () =>
    new Response(zip, { headers: { 'Content-Type': 'application/zip' } }),
  )

  const archive = await api.diagnosticBundle()
  assert.deepEqual(new Uint8Array(await archive.arrayBuffer()), zip)
  assert.equal(fetch.mock.calls.length, 1)
  const [url, init] = fetch.mock.calls[0]!.arguments as unknown as [string, RequestInit]
  assert.equal(url, '/api/diagnostics/bundle')
  assert.equal(init.method, 'GET')
  assert.equal(init.credentials, 'same-origin')
  assert.equal(init.headers, undefined)
})

test('an expired archive session invokes the panel login handler', async (t) => {
  const signIn = t.mock.fn()
  setUnauthorizedHandler(signIn)
  t.after(() => setUnauthorizedHandler(() => {}))
  t.mock.method(globalThis, 'fetch', async () =>
    Response.json({ error: 'token login required' }, { status: 401 }),
  )

  await assert.rejects(api.diagnosticBundle(), { status: 401 })
  assert.equal(signIn.mock.callCount(), 1)
})

test('does not save a successful HTML response as a diagnostics ZIP', async (t) => {
  t.mock.method(globalThis, 'fetch', async () =>
    new Response('<html>Panel</html>', { headers: { 'Content-Type': 'text/html' } }),
  )

  await assert.rejects(api.diagnosticBundle(), /ZIP archive/)
})

test('rejects a diagnostics response without a content type', async (t) => {
  t.mock.method(globalThis, 'fetch', async () => new Response(null))
  await assert.rejects(api.diagnosticBundle(), /ZIP archive/)
})

test('static demo never calls the router archive endpoint', async (t) => {
  const fetch = t.mock.method(globalThis, 'fetch', async () => assert.fail('unexpected fetch'))
  await assert.rejects(demoAPI.diagnosticBundle(), /router/)
  assert.equal(fetch.mock.callCount(), 0)
})
