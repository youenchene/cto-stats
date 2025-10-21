import React from 'react'

export type Point = { label: string; value: number }

export function LineChart({
  series,
  width = 800,
  height = 200,
  colors = ['#000'],
  yTicks = 4,
  showAxes = true,
}: {
  series: Point[][]
  width?: number
  height?: number
  colors?: string[]
  yTicks?: number
  showAxes?: boolean
}) {
  const allValues = series.flat().map((p) => p.value).filter((n) => Number.isFinite(n))
  if (allValues.length === 0) return <svg width={width} height={height} />
  const min = Math.min(...allValues)
  const max = Math.max(...allValues)
  const rng = max - min || 1
  const count = Math.max(0, Math.max(...series.map((s) => s.length)))
  const padding = 32
  const innerW = width - padding * 2
  const innerH = height - padding * 2

  const x = (i: number) => padding + (count <= 1 ? innerW / 2 : (i * innerW) / (count - 1))
  const y = (v: number) => padding + (innerH - ((v - min) / rng) * innerH)

  const xLabels = count > 0 ? [0, Math.floor((count - 1) / 2), count - 1] : []

  const paths = series.map((s, idx) => {
    const d = s
      .map((p, i) => `${i === 0 ? 'M' : 'L'}${x(i).toFixed(2)},${y(p.value).toFixed(2)}`)
      .join(' ')
    return <path key={idx} d={d} fill="none" stroke={colors[idx % colors.length]} strokeWidth={2} />
  })

  const yTickVals = Array.from({ length: yTicks + 1 }, (_, i) => min + (i * (rng)) / yTicks)

  return (
    <svg width={width} height={height} className="chart-grayscale">
      {showAxes && (
        <g>
          {/* axes */}
          <line x1={padding} y1={padding} x2={padding} y2={height - padding} stroke="#e5e7eb" />
          <line x1={padding} y1={height - padding} x2={width - padding} y2={height - padding} stroke="#e5e7eb" />
          {/* y ticks */}
          {yTickVals.map((v, i) => (
            <g key={i}>
              <line x1={padding - 4} y1={y(v)} x2={padding} y2={y(v)} stroke="#e5e7eb" />
              <text x={8} y={y(v)} textAnchor="start" alignmentBaseline="middle" fontSize="10" fill="#6b7280">
                {v.toFixed(0)}
              </text>
            </g>
          ))}
          {/* x labels */}
          {xLabels.map((i) => (
            <text key={i} x={x(i)} y={height - padding + 14} textAnchor="middle" fontSize="10" fill="#6b7280">
              {series[0]?.[i]?.label ?? ''}
            </text>
          ))}
        </g>
      )}
      {/* series paths */}
      {paths}
    </svg>
  )
}
