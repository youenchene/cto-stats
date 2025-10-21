import React from 'react'

export type StackSeries = { name: string; values: number[] }

export function StackedBarChart({
  labels,
  stacks,
  width = 900,
  height = 220,
  colors = ['#1f77b4', '#ff7f0e', '#2ca02c', '#d62728', '#9467bd', '#8c564b', '#e377c2', '#7f7f7f', '#bcbd22', '#17becf'],
  yTicks = 4,
  showAxes = true,
  barGap = 6,
}: {
  labels: string[]
  stacks: StackSeries[]
  width?: number
  height?: number
  colors?: string[]
  yTicks?: number
  showAxes?: boolean
  barGap?: number
}) {
  const count = labels.length
  const padding = 32
  const innerW = Math.max(0, width - padding * 2)
  const innerH = Math.max(0, height - padding * 2)

  if (!count || !stacks.length) return <svg width={width} height={height} />

  // compute totals per bar
  const totals: number[] = Array.from({ length: count }, (_, i) =>
    stacks.reduce((acc, s) => acc + (Number.isFinite(s.values[i]) ? (s.values[i] || 0) : 0), 0)
  )
  const maxTotal = Math.max(1, ...totals)

  const x = (i: number) => padding + (i * innerW) / count
  const y = (v: number) => padding + (innerH - (v / maxTotal) * innerH)

  const barW = innerW / count - barGap

  const yTickVals = Array.from({ length: yTicks + 1 }, (_, i) => (i * maxTotal) / yTicks)

  let colorMap = new Map<string, string>()
  stacks.forEach((s, idx) => {
    colorMap.set(s.name, colors[idx % colors.length])
  })

  return (
    <svg width={width} height={height} className="chart-grayscale">
      {showAxes && (
        <g>
          <line x1={padding} y1={padding} x2={padding} y2={height - padding} stroke="#e5e7eb" />
          <line x1={padding} y1={height - padding} x2={width - padding} y2={height - padding} stroke="#e5e7eb" />
          {yTickVals.map((v, i) => (
            <g key={i}>
              <line x1={padding - 4} y1={y(v)} x2={padding} y2={y(v)} stroke="#e5e7eb" />
              <text x={8} y={y(v)} textAnchor="start" alignmentBaseline="middle" fontSize="10" fill="#6b7280">
                {v.toFixed(0)}
              </text>
            </g>
          ))}
          {/* Show first, middle, last labels */}
          {[0, Math.floor((count - 1) / 2), count - 1].filter((i, idx, arr) => i >= 0 && (idx === 0 || i !== arr[idx - 1])).map((i) => (
            <text key={i} x={x(i) + barW / 2} y={height - padding + 14} textAnchor="middle" fontSize="10" fill="#6b7280">
              {labels[i]}
            </text>
          ))}
        </g>
      )}
      {labels.map((_, i) => {
        let acc = 0
        const bx = x(i) + barGap / 2
        const parts = stacks.map((s, idx) => {
          const v = s.values[i] || 0
          const y1 = y(acc)
          const y2 = y(acc + v)
          const h = Math.max(0, y1 - y2)
          const rect = (
            <rect
              key={s.name}
              x={bx}
              y={y2}
              width={Math.max(0, barW)}
              height={h}
              fill={colorMap.get(s.name)}
            />
          )
          acc += v
          return rect
        })
        return <g key={i}>{parts}</g>
      })}
    </svg>
  )
}
