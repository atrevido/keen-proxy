import { ApiError } from '../apiError.ts'
import type { UpdatePreferences, UpdateSelection, UpdateStatus } from '../types'

const latest = 'v1.2.0'
const installDelayMs = 3000

function scheduled(prefs: UpdatePreferences, current: string, clock: string) {
  return prefs.enabled && current !== latest && prefs.time === clock.slice(11, 16)
}

function selectedRelease(selection: UpdateSelection, status: UpdateStatus) {
  const target = selection.rollback ? status.previous : status.latest
  if (!target || target !== selection.target || status.job?.state === 'running') {
    throw new ApiError(409, 'The simulated update state changed')
  }
  return target
}

// Simulate the updater entirely in this page, including the daily clock check.
export function createDemoUpdates() {
  let current = 'v1.1.0'
  let previous = 'v1.0.0'
  let prefs: UpdatePreferences = { enabled: false, time: '04:00' }
  let job: UpdateStatus['job']
  let completeAt = 0
  let checkedAt = new Date().toISOString()

  function begin(target: string, now: number) {
    job = { state: 'running', target }
    completeAt = now + installDelayMs
  }

  function advance(now: number, clock: string) {
    if (job?.state === 'running') {
      if (now >= completeAt) {
        previous = current
        current = job.target
        job = { state: 'success', target: current }
      }
      return
    }
    if (scheduled(prefs, current, clock)) {
      begin(latest, now)
    }
  }

  function status(): UpdateStatus {
    const now = Date.now()
    const clock = new Date(now).toISOString()
    advance(now, clock)
    return {
      available: true, demo: true, current, previous,
      latest: current === latest ? undefined : latest,
      checked_at: checkedAt, prefs: { ...prefs }, job: job && { ...job }, router_time: clock,
    }
  }

  function install(selection: UpdateSelection): UpdateStatus {
    const target = selectedRelease(selection, status())
    if (selection.rollback) prefs.enabled = false
    begin(target, Date.now())
    return status()
  }

  function save(next: UpdatePreferences): UpdateStatus {
    if (!/^(?:[01]\d|2[0-3]):[0-5]\d$/.test(next.time)) {
      throw new ApiError(400, 'Expected time in HH:MM format')
    }
    prefs = { ...next }
    return status()
  }

  function check(): UpdateStatus {
    checkedAt = new Date().toISOString()
    return status()
  }

  return { status, check, install, save }
}
