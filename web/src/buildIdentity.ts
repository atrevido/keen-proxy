import type { BuildIdentity } from './types'

// Keep the build label consistent in the footer, Settings, and Diagnostics.
export function formatBuild(build: BuildIdentity): string {
  const revision = build.revision && (build.dirty ? `${build.revision}+dirty` : build.revision)
  return [build.variant, build.arch.toUpperCase(), revision].filter(Boolean).join(' · ')
}
