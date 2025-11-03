import React, { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

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
  yMax,
}: {
  labels: string[]
  stacks: StackSeries[]
  width?: number
  height?: number
  colors?: string[]
  yTicks?: number
  showAxes?: boolean
  barGap?: number
  yMax?: number
}) {
  const { t } = useTranslation()
  const count = labels.length
  const padding = 32
  const innerW = Math.max(0, width - padding * 2)
  const innerH = Math.max(0, height - padding * 2)

  const [hover, setHover] = useState<number | null>(null)


  // compute totals per bar
  const totals: number[] = Array.from({ length: count }, (_, i) =>
    stacks.reduce((acc, s) => acc + (Number.isFinite(s.values[i]) ? (s.values[i] || 0) : 0), 0)
  )
  const dataMax = Math.max(1, ...totals)
  const maxTotal = Math.max(1, yMax ?? dataMax)

  const x = (i: number) => padding + (i * innerW) / count
  const y = (v: number) => padding + (innerH - (v / maxTotal) * innerH)

  const barW = innerW / count - barGap

  const yTickVals = Array.from({ length: yTicks + 1 }, (_, i) => (i * maxTotal) / yTicks)

  // dynamic x ticks (5 to 10 depending on width)
  const xTickIndices = useMemo(() => {
    if (count === 0) return [] as number[]
    const desired = Math.min(10, Math.max(5, Math.floor(innerW / 120)))
    const tickCount = Math.min(count, desired)
    if (tickCount <= 1) return [0]
    const idxs: number[] = []
    for (let k = 0; k < tickCount; k++) {
      const idx = Math.round((k * (count - 1)) / (tickCount - 1))
      if (idxs[idxs.length - 1] !== idx) idxs.push(idx)
    }
    return idxs
  }, [count, innerW])

  let colorMap = new Map<string, string>()
  stacks.forEach((s, idx) => {
    colorMap.set(s.name, colors[idx % colors.length])
  })

  const onMouseMove = (e: React.MouseEvent<SVGSVGElement, MouseEvent>) => {
    const rect = (e.currentTarget as SVGSVGElement).getBoundingClientRect()
    const px = e.clientX - rect.left
    const rel = (px - padding) / innerW
    const idx = Math.floor(rel * count)
    if (idx >= 0 && idx < count) setHover(idx)
    else setHover(null)
  }
  const onMouseLeave = () => setHover(null)

  // tooltip data for hovered bar
  const hoverData = hover != null ? {
    index: hover,
    label: labels[hover],
    parts: stacks.map((s) => ({ name: s.name, value: s.values[hover] || 0, color: colorMap.get(s.name)! })),
    total: totals[hover] || 0,
  } : null

  return (
    <svg width={width} height={height} className="chart-grayscale" onMouseMove={onMouseMove} onMouseLeave={onMouseLeave}>
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
          {/* x grid + labels */}
          {xTickIndices.map((i, k) => (
            <g key={k}>
              <line x1={x(i) + barW / 2} y1={padding} x2={x(i) + barW / 2} y2={height - padding} stroke="#f3f4f6" />
              <text x={x(i) + barW / 2} y={height - padding + 14} textAnchor="middle" fontSize="10" fill="#6b7280">
                {labels[i]}
              </text>
            </g>
          ))}
        </g>
      )}
      {labels.map((_, i) => {
        let acc = 0
        const bx = x(i) + barGap / 2
        const parts = stacks.map((s) => {
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

      {/* hover overlay */}
      {hover != null && (
        <g>
          <rect x={x(hover) + barGap / 2} y={padding} width={Math.max(0, barW)} height={innerH} fill="#000" opacity={0.06} />
          {/* tooltip */}
          {hoverData && (
            (() => {
              const bx = x(hover) + barGap / 2
              const tooltipW = 180
              // Add extra spacing between the title and the legend items
              const titleLineHeight = 16
              const gapBelowTitle = 18
              const itemLineHeight = 16
              const tooltipH = titleLineHeight + gapBelowTitle + hoverData.parts.length * itemLineHeight + 12
              const tx = Math.min(width - padding - tooltipW, bx + barW + 8)
              const ty = padding + 8
              const titleY = ty + titleLineHeight
              const firstItemBaseY = titleY + gapBelowTitle
              return (
                <g>
                  <rect x={tx} y={ty} width={tooltipW} height={tooltipH} fill="#ffffff" stroke="#e5e7eb" rx={6} ry={6} />
                  <text x={tx + 8} y={titleY} fontSize="11" fill="#111827">{hoverData.label} — {t('chart.total')} {hoverData.total.toFixed(0)}</text>
                  {hoverData.parts.map((p, idx) => (
                    <g key={idx}>
                      <rect x={tx + 8} y={firstItemBaseY + idx * itemLineHeight - 8} width={8} height={8} fill={p.color} />
                      <text x={tx + 20} y={firstItemBaseY + idx * itemLineHeight} fontSize="11" fill="#374151">
                        {p.name}: {p.value.toFixed(0)}{hoverData.total > 0 ? ` (${Math.round((p.value / hoverData.total) * 100)}%)` : ''}
                      </text>
                    </g>
                  ))}
                </g>
              )
            })()
          )}
        </g>
      )}
    </svg>
  )
}
