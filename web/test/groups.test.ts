import { test } from 'node:test'
import assert from 'node:assert/strict'
import { groupKey, changeGroupTargets } from '../src/groups.ts'
import type { Group } from '../src/types.ts'
test('selection follows identity after deletion, including hidden and disabled groups', () => {
  const first: Group = { name: 'first', target: 'old', entries: [] }
  const hidden: Group = {
    name: 'hidden',
    target: 'old',
    entries: [],
    disabled: true,
  }
  const country: Group = {
    name: 'country',
    target: 'old',
    entries: [],
    countries: ['RU'],
    except: true,
  }
  const groups = [first, hidden, country],
    selected = new Set([groupKey(hidden), groupKey(country)])
  groups.shift()
  changeGroupTargets(groups, selected, 'new', ['old', 'new'])
  assert.equal(hidden.target, 'new')
  assert.equal(country.target, 'new')
  assert.equal(hidden.disabled, true)
  assert.equal(first.target, 'old')
  assert.equal(groupKey(hidden), [...selected][0])
})
test('invalid target cannot partially change selected groups', () => {
  const g: Group = { name: 'x', target: 'old', entries: [] }
  assert.throws(() =>
    changeGroupTargets([g], new Set([groupKey(g)]), 'missing', ['old']),
  )
  assert.equal(g.target, 'old')
})
test('identical groups have distinct stable selection identities', () => {
  const a: Group = { name: 'same', target: 'old', entries: [] },
    b: Group = { ...a }
  assert.equal(typeof groupKey(a), 'string')
  assert.notEqual(groupKey(a), groupKey(b))
  changeGroupTargets([a, b], new Set([groupKey(a)]), 'new', ['old', 'new'])
  assert.equal(a.target, 'new')
  assert.equal(b.target, 'old')
})
