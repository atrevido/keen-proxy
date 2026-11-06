<script setup lang="ts">
// Lightweight SVG chart with smoothed lines, gradient fills, and hover details.
import { computed, ref } from 'vue'
import { hhmm } from '../format'

export interface Series {
  name: string
  color: string
  values: number[]
  fill?: boolean
}

const props = withDefaults(
  defineProps<{ times: number[]; series: Series[]; height?: number; format?: (v: number) => string; maxPoints?: number }>(),
  { height: 260, format: (v: number) => String(Math.round(v)), maxPoints: 300 },
)

const W = 1000 // Internal viewBox width; the SVG scales to its container.
const PAD = { l: 44, r: 12, t: 12, b: 28 }
const uid = Math.random().toString(36).slice(2)

// Downsample dense series to bound SVG path size.
const data = computed(() => {
  const n = props.times.length
  const step = Math.max(1, Math.ceil(n / props.maxPoints))
  const idx: number[] = []
  for (let i = 0; i < n; i += step) idx.push(i)
  if (n && idx[idx.length - 1] !== n - 1) idx.push(n - 1)
  return {
    times: idx.map((i) => props.times[i]),
    series: props.series.map((s) => ({ ...s, values: idx.map((i) => s.values[i] ?? 0) })),
  }
})

const maxY = computed(() => {
  let m = 0
  for (const s of data.value.series) for (const v of s.values) m = Math.max(m, v)
  if (m <= 0) return 1
  // Round the upper bound to 1, 2, 2.5, or 5 times a power of ten.
  const p = Math.pow(10, Math.floor(Math.log10(m)))
  for (const k of [1, 2, 2.5, 5, 10]) if (m <= k * p) return k * p
  return 10 * p
})

const plotH = computed(() => props.height - PAD.t - PAD.b)
const x = (i: number) => PAD.l + (i / Math.max(1, data.value.times.length - 1)) * (W - PAD.l - PAD.r)
const y = (v: number) => PAD.t + plotH.value - (v / maxY.value) * plotH.value

// Interpolate neighboring points with Catmull-Rom-derived Bezier segments.
function path(values: number[]): string {
  if (!values.length) return ''
  const pts = values.map((v, i) => [x(i), y(v)])
  let d = `M${pts[0][0]},${pts[0][1]}`
  for (let i = 0; i < pts.length - 1; i++) {
    const p0 = pts[i - 1] ?? pts[i]
    const p1 = pts[i]
    const p2 = pts[i + 1]
    const p3 = pts[i + 2] ?? p2
    const c1x = p1[0] + (p2[0] - p0[0]) / 6
    const c1y = Math.max(PAD.t, Math.min(y(0), p1[1] + (p2[1] - p0[1]) / 6))
    const c2x = p2[0] - (p3[0] - p1[0]) / 6
    const c2y = Math.max(PAD.t, Math.min(y(0), p2[1] - (p3[1] - p1[1]) / 6))
    d += ` C${c1x},${c1y} ${c2x},${c2y} ${p2[0]},${p2[1]}`
  }
  return d
}

const grid = computed(() => [0, 1, 2, 3].map((k) => (maxY.value * k) / 3))
const xTicks = computed(() => {
  const n = data.value.times.length
  if (n < 2) return []
  return [0, 0.25, 0.5, 0.75, 1].map((f) => {
    const i = Math.round(f * (n - 1))
    return { x: x(i), label: hhmm(data.value.times[i]) }
  })
})

const hover = ref<number | null>(null)
const svg = ref<SVGSVGElement | null>(null)
function onMove(e: MouseEvent) {
  const el = svg.value
  if (!el || data.value.times.length < 2) return
  const r = el.getBoundingClientRect()
  const vx = ((e.clientX - r.left) / r.width) * W
  const f = (vx - PAD.l) / (W - PAD.l - PAD.r)
  hover.value = Math.max(0, Math.min(data.value.times.length - 1, Math.round(f * (data.value.times.length - 1))))
}
const tip = computed(() => {
  if (hover.value === null) return null
  const i = hover.value
  return {
    left: `${(x(i) / W) * 100}%`,
    time: hhmm(data.value.times[i]),
    rows: data.value.series.map((s) => ({ name: s.name, color: s.color, value: props.format(s.values[i]) })),
  }
})
</script>

<template>
  <div class="chart" :style="{ height: height + 'px' }">
    <svg
      ref="svg"
      :viewBox="`0 0 ${W} ${height}`"
      preserveAspectRatio="none"
      @mousemove="onMove"
      @mouseleave="hover = null"
    >
      <defs>
        <linearGradient v-for="(s, k) in data.series" :id="`g-${uid}-${k}`" :key="k" x1="0" x2="0" y1="0" y2="1">
          <stop offset="0%" :stop-color="s.color" stop-opacity="0.28" />
          <stop offset="100%" :stop-color="s.color" stop-opacity="0.02" />
        </linearGradient>
      </defs>
      <g class="grid">
        <line v-for="g in grid" :key="g" :x1="PAD.l" :x2="W - PAD.r" :y1="y(g)" :y2="y(g)" />
      </g>
      <g v-for="(s, k) in data.series" :key="s.name">
        <path
          v-if="s.fill !== false && s.values.length"
          :d="`${path(s.values)} L${x(s.values.length - 1)},${y(0)} L${x(0)},${y(0)} Z`"
          :fill="`url(#g-${uid}-${k})`"
        />
        <path :d="path(s.values)" fill="none" :stroke="s.color" stroke-width="2" vector-effect="non-scaling-stroke" />
      </g>
      <line v-if="hover !== null" class="cursor" :x1="x(hover)" :x2="x(hover)" :y1="PAD.t" :y2="y(0)" />
    </svg>
    <div class="ylabels">
      <span v-for="g in grid" :key="g" :style="{ top: `${(y(g) / height) * 100}%` }">{{ format(g) }}</span>
    </div>
    <div class="xlabels">
      <span v-for="t in xTicks" :key="t.label + t.x" :style="{ left: `${(t.x / W) * 100}%` }">{{ t.label }}</span>
    </div>
    <div v-if="tip" class="tip" :style="{ left: tip.left }">
      <div class="tip-time">{{ tip.time }}</div>
      <div v-for="r in tip.rows" :key="r.name" class="tip-row">
        <i :style="{ background: r.color }" />{{ r.name }} <b>{{ r.value }}</b>
      </div>
    </div>
  </div>
</template>

<style scoped>
.chart {
  position: relative;
  width: 100%;
  user-select: none;
}
svg {
  width: 100%;
  height: 100%;
  display: block;
}
.grid line {
  stroke: var(--line);
  stroke-dasharray: 4 6;
  vector-effect: non-scaling-stroke;
}
.cursor {
  stroke: var(--muted);
  stroke-width: 1;
  vector-effect: non-scaling-stroke;
}
.ylabels span,
.xlabels span {
  position: absolute;
  font-size: 10px;
  color: var(--muted);
  white-space: nowrap;
}
.ylabels span {
  left: 0;
  width: 38px;
  text-align: right;
  transform: translateY(-50%);
}
.xlabels span {
  bottom: 4px;
  transform: translateX(-50%);
}
.tip {
  position: absolute;
  top: 8px;
  transform: translateX(-50%);
  background: var(--card);
  border: 1px solid var(--line);
  border-radius: 8px;
  padding: 6px 10px;
  font-size: 12px;
  pointer-events: none;
  white-space: nowrap;
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.25);
}
.tip-time {
  color: var(--muted);
  margin-bottom: 2px;
}
.tip-row {
  display: flex;
  align-items: center;
  gap: 6px;
}
.tip-row i {
  width: 8px;
  height: 8px;
  border-radius: 2px;
}
</style>
