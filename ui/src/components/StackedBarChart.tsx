import React, { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { formatNumber } from '../lib/utils'

export type StackSeries = { name: string; values: number[]; color?: string }

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
  yAxisLabel,
  showLegend = false,
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
  yAxisLabel?: string
  showLegend?: boolean
}) {
  const { t } = useTranslation()
  const count = labels.length
  const padding = 32
  // Add extra top gap below the Y-axis label to create a visual margin (≈2em)
  const topGap = yAxisLabel ? 32 : 0
  // Reserve space for an optional legend row (placed under the chart)
  const legendHeight = showLegend ? 22 : 0
  const paddingTop = padding + topGap
  const paddingBottom = padding + legendHeight
  // Add extra left padding when a Y-axis label is present to create more space
  const paddingLeft = yAxisLabel ? 64 : padding
  const paddingRight = padding
  const innerW = Math.max(0, width - paddingLeft - paddingRight)
  const innerH = Math.max(0, height - paddingTop - paddingBottom)

  const [hover, setHover] = useState<number | null>(null)


  // compute totals per bar
  const totals: number[] = Array.from({ length: count }, (_, i) =>
    stacks.reduce((acc, s) => acc + (Number.isFinite(s.values[i]) ? (s.values[i] || 0) : 0), 0)
  )
  const dataMax = Math.max(1, ...totals)
  const maxTotal = Math.max(1, yMax ?? dataMax)

  const x = (i: number) => paddingLeft + (i * innerW) / count
  const y = (v: number) => paddingTop + (innerH - (v / maxTotal) * innerH)

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
    colorMap.set(s.name, s.color ?? colors[idx % colors.length])
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
          <line x1={paddingLeft} y1={paddingTop} x2={paddingLeft} y2={height - paddingBottom} stroke="#e5e7eb" />
          <line x1={paddingLeft} y1={height - paddingBottom} x2={width - paddingRight} y2={height - paddingBottom} stroke="#e5e7eb" />
          {yAxisLabel && (
            <text
              x={8}
              y={padding - 8}
              textAnchor="start"
              alignmentBaseline="baseline"
              fontSize="11"
              fill="#374151"
            >
              {yAxisLabel}
            </text>
          )}
          {yTickVals.map((v, i) => (
            <g key={i}>
              <line x1={paddingLeft - 4} y1={y(v)} x2={paddingLeft} y2={y(v)} stroke="#e5e7eb" />
              <text x={paddingLeft - 10} y={y(v)} textAnchor="end" alignmentBaseline="middle" fontSize="10" fill="#6b7280">
                {formatNumber(v)}
              </text>
            </g>
          ))}
          {/* x grid + labels */}
          {xTickIndices.map((i, k) => (
            <g key={k}>
              <line x1={x(i) + barW / 2} y1={paddingTop} x2={x(i) + barW / 2} y2={height - paddingBottom} stroke="#f3f4f6" />
              <text x={x(i) + barW / 2} y={height - paddingBottom + 14} textAnchor="middle" fontSize="10" fill="#6b7280">
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
          <rect x={x(hover) + barGap / 2} y={paddingTop} width={Math.max(0, barW)} height={innerH} fill="#000" opacity={0.06} />
          {/* tooltip */}
          {hoverData && (
            (() => {
              const bx = x(hover) + barGap / 2
              const tooltipW = 180
              // Add extra spacing between the title and the legend items
              const titleLineHeight = 16
              const gapBelowTitle = 18
              const itemLineHeight = 16
              const items = (hoverData.total > 0
                ? hoverData.parts
                    .map((p) => ({
                      ...p,
                      percent: Math.round((p.value / hoverData.total) * 100),
                    }))
                    .filter((p) => p.percent > 0)
                : [])
              const tooltipH = titleLineHeight + gapBelowTitle + items.length * itemLineHeight + 12
              const tx = Math.min(width - padding - tooltipW, bx + barW + 8)
              const ty = paddingTop + 8
              const titleY = ty + titleLineHeight
              const firstItemBaseY = titleY + gapBelowTitle
              return (
                <g>
                  <rect x={tx} y={ty} width={tooltipW} height={tooltipH} fill="#ffffff" stroke="#e5e7eb" rx={6} ry={6} />
                  <text x={tx + 8} y={titleY} fontSize="11" fill="#111827">{hoverData.label} — {t('chart.total')} {formatNumber(hoverData.total)}</text>
                  {items.map((p, idx) => (
                    <g key={idx}>
                      <rect x={tx + 8} y={firstItemBaseY + idx * itemLineHeight - 8} width={8} height={8} fill={p.color} />
                      <text x={tx + 20} y={firstItemBaseY + idx * itemLineHeight} fontSize="11" fill="#374151">
                        {p.name}: {formatNumber(p.value)} ({p.percent}%)
                      </text>
                    </g>
                  ))}
                </g>
              )
            })()
          )}
        </g>
      )}

      {/* Legend under the chart */}
      {showLegend && stacks.length > 0 && (
        (() => {
          const itemW = 120 // fixed width per legend item
          const startX = paddingLeft
          const rectY = height - legendHeight + 6
          const textY = rectY + 12
          return (
            <g>
              {stacks.map((s, i) => (
                <g key={s.name}>
                  <rect x={startX + i * itemW} y={rectY} width={10} height={10} fill={colorMap.get(s.name)} rx={2} ry={2} />
                  <text x={startX + i * itemW + 16} y={textY} fontSize="11" fill="#374151">
                    {s.name}
                  </text>
                </g>
              ))}
            </g>
          )
        })()
      )}
    </svg>
  )
}
