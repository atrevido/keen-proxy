import assert from 'node:assert/strict'
import { test } from 'node:test'
import { api, ApiError, setUnauthorizedHandler } from '../src/api.ts'

test('update commands use the session and send the confirmed release tag', async (t) => {
  const calls: { url: string; method?: string; credentials?: string; body: unknown }[] = []
  const status = {
    available: true,
    current: 'v0.3.0',
    latest: 'v0.4.0',
    prefs: { enabled: false, time: '03:00' },
  }
  t.mock.method(globalThis, 'fetch', async (url: string, init: RequestInit) => {
    calls.push({ url, method: init.method, credentials: init.credentials, body: init.body && JSON.parse(String(init.body)) })
    return Response.json(status)
  })
  assert.deepEqual(await api.updates(), status)
  await api.checkUpdates()
  await api.saveUpdatePreferences({ enabled: true, time: '04:30' })
  await api.installUpdate({ rollback: false, target: 'v0.4.0' })
  await api.installUpdate({ rollback: true, target: 'v0.2.0' })
  assert.deepEqual(calls, [
    { url: '/api/updates', method: 'GET', credentials: 'same-origin', body: undefined },
    { url: '/api/updates/check', method: 'POST', credentials: 'same-origin', body: {} },
    { url: '/api/updates/preferences', method: 'PUT', credentials: 'same-origin', body: { enabled: true, time: '04:30' } },
    { url: '/api/updates/install', method: 'POST', credentials: 'same-origin', body: { rollback: false, target: 'v0.4.0' } },
    { url: '/api/updates/install', method: 'POST', credentials: 'same-origin', body: { rollback: true, target: 'v0.2.0' } },
  ])
})

test('a token challenge for an update can be handled in place', async (t) => {
  setUnauthorizedHandler(() => assert.fail('A token challenge should keep the settings page open'))
  t.after(() => setUnauthorizedHandler(() => {}))
  t.mock.method(globalThis, 'fetch', async () => Response.json({ error: 'token required' }, { status: 403 }))
  await assert.rejects(api.installUpdate({ rollback: false, target: 'v0.4.0' }), (error) => {
    assert.ok(error instanceof ApiError)
    assert.equal(error.status, 403)
    assert.equal(error.message, 'token required')
    return true
  })
})

test('an older daemon without an update route returns a detectable 404', async (t) => {
  t.mock.method(globalThis, 'fetch', async () => new Response('404 page not found\n', { status: 404 }))
  await assert.rejects(api.updates(), (error) => {
    assert.ok(error instanceof ApiError)
    assert.equal(error.status, 404)
    assert.equal(error.message, 'HTTP 404')
    return true
  })
})

test('an empty server error still shows the HTTP status', async (t) => {
  t.mock.method(globalThis, 'fetch', async () => Response.json({ error: '' }, { status: 503 }))
  await assert.rejects(api.updates(), { status: 503, message: 'HTTP 503' })
})

test('a rejected token retry stays local but an expired session requests login', async (t) => {
  const signIn = t.mock.fn()
  setUnauthorizedHandler(signIn)
  t.after(() => setUnauthorizedHandler(() => {}))
  t.mock.method(globalThis, 'fetch', async () => Response.json({ error: 'invalid token' }, { status: 401 }))
  await assert.rejects(api.login('wrong-token'), { status: 401 })
  assert.equal(signIn.mock.callCount(), 0)
  await assert.rejects(api.updates(), { status: 401 })
  assert.equal(signIn.mock.callCount(), 1)
})
