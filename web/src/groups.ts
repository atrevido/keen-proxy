import type { Group } from './types'

// Session identity deliberately stays out of the persisted configuration.
const keys = new WeakMap<Group, string>()
let nextKey = 0
// Keep selection stable across renames without persisting UI identity.
export function groupKey(group: Group): string {
  let key = keys.get(group)
  if (!key) {
    key = `group-${++nextKey}`
    keys.set(group, key)
  }
  return key
}

// Validate the destination once, then update selected groups including hidden ones.
export function changeGroupTargets(
  groups: Group[],
  selected: Set<string>,
  target: string,
  allowed: string[],
): void {
  if (!allowed.includes(target)) throw new Error('Unknown target')
  for (const group of groups)
    if (selected.has(groupKey(group))) group.target = target
}
