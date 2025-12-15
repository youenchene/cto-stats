import React, { useMemo } from 'react'
import { formatNumber } from '../lib/utils'

export type Point = { label: string; value: number }

export function LineChart({
  series,
  width = 800,
  height = 200,
  colors = ['#000'],
  yTicks = 4,
  showAxes = true,
  xTickCount,
  showOOCMarkers = true,
  yAxisTitle,
  showBandLabels = true,
  uclLabel = 'UCL',
  lclLabel = 'LCL',
}: {
  series: Point[][]
  width?: number
  height?: number
  colors?: string[]
  yTicks?: number
  showAxes?: boolean
  xTickCount?: number
  showOOCMarkers?: boolean
  yAxisTitle?: string
  showBandLabels?: boolean
  uclLabel?: string
  lclLabel?: string
}) {
  const allValues = series.flat().map((p) => p.value).filter((n) => Number.isFinite(n))
  if (allValues.length === 0) return <svg width={width} height={height} />
  const min = Math.min(...allValues)
  const max = Math.max(...allValues)
  const rng = max - min || 1
  const count = Math.max(0, Math.max(...series.map((s) => s.length)))
  const basePadding = 32
  // Add some extra left padding when a Y-axis title is present so it doesn't overlap ticks
  const leftPad = basePadding + (yAxisTitle ? 28 : 0)
  const padding = basePadding
  const innerW = width - leftPad - padding
  const innerH = height - padding * 2

  const x = (i: number) => leftPad + (count <= 1 ? innerW / 2 : (i * innerW) / (count - 1))
  const y = (v: number) => padding + (innerH - ((v - min) / rng) * innerH)

  // dynamic x ticks; if xTickCount provided, force that count; otherwise 5 to 10 depending on width
  const xTickIndices = useMemo(() => {
    if (count === 0) return [] as number[]
    const desired = xTickCount && xTickCount > 0 ? xTickCount : Math.min(10, Math.max(5, Math.floor(innerW / 120)))
    const tickCount = Math.min(count, desired)
    if (tickCount <= 1) return [0]
    const idxs: number[] = []
    for (let k = 0; k < tickCount; k++) {
      const idx = Math.round((k * (count - 1)) / (tickCount - 1))
      if (idxs[idxs.length - 1] !== idx) idxs.push(idx)
    }
    return idxs
  }, [count, innerW, xTickCount])

  const paths = series.map((s, idx) => {
    const d = s
      .map((p, i) => `${i === 0 ? 'M' : 'L'}${x(i).toFixed(2)},${y(p.value).toFixed(2)}`)
      .join(' ')
    return <path key={idx} d={d} fill="none" stroke={colors[idx % colors.length]} strokeWidth={2} />
  })

  const yTickVals = Array.from({ length: yTicks + 1 }, (_, i) => min + (i * rng) / yTicks)

  return (
    <svg width={width} height={height} className="chart-grayscale">
      {showAxes && (
        <g>
          {/* optional Y-axis title */}
          {yAxisTitle && (
            <text
              x={12}
              y={height / 2}
              transform={`rotate(-90 12 ${height / 2})`}
              textAnchor="middle"
              fontSize="11"
              fill="#111827"
            >
              {yAxisTitle}
            </text>
          )}
          {/* axes */}
          <line x1={leftPad} y1={padding} x2={leftPad} y2={height - padding} stroke="#e5e7eb" />
          <line x1={leftPad} y1={height - padding} x2={width - padding} y2={height - padding} stroke="#e5e7eb" />
          {/* y ticks */}
          {yTickVals.map((v, i) => (
            <g key={i}>
              <line x1={leftPad - 4} y1={y(v)} x2={leftPad} y2={y(v)} stroke="#e5e7eb" />
              <text x={leftPad - 4 - 6} y={y(v)} textAnchor="end" alignmentBaseline="middle" fontSize="10" fill="#6b7280">
                {formatNumber(v)}
              </text>
            </g>
          ))}
          {/* x grid + labels */}
          {xTickIndices.map((i, k) => (
            <g key={k}>
              <line x1={x(i)} y1={padding} x2={x(i)} y2={height - padding} stroke="#f3f4f6" />
              <text x={x(i)} y={height - padding + 14} textAnchor="middle" fontSize="10" fill="#6b7280">
                {series[0]?.[i]?.label ?? ''}
              </text>
            </g>
          ))}
        </g>
      )}
      {/* background bands: light blue/cyan between LCL and UCL, light red outside */}
      {series.length >= 3 && (
        <g>
          {(() => {
            const lcl = series[1]
            const ucl = series[2]
            // helpers to build polygons for contiguous finite ranges
            const isFiniteAt = (s: Point[], i: number) => Number.isFinite(s?.[i]?.value)
            const buildBandPolys = () => {
              const polys: string[] = []
              let start = -1
              const last = Math.max(lcl.length, ucl.length) - 1
              for (let i = 0; i <= last; i++) {
                const ok = isFiniteAt(lcl, i) && isFiniteAt(ucl, i)
                if (ok && start === -1) start = i
                if ((!ok || i === last) && start !== -1) {
                  const end = ok && i === last ? i : i - 1
                  // build polygon from UCL (start->end) then back on LCL (end->start)
                  const parts: string[] = []
                  parts.push(`M${x(start).toFixed(2)},${y(ucl[start].value).toFixed(2)}`)
                  for (let k = start + 1; k <= end; k++) {
                    parts.push(`L${x(k).toFixed(2)},${y(ucl[k].value).toFixed(2)}`)
                  }
                  for (let k = end; k >= start; k--) {
                    parts.push(`L${x(k).toFixed(2)},${y(lcl[k].value).toFixed(2)}`)
                  }
                  parts.push('Z')
                  polys.push(parts.join(' '))
                  start = -1
                }
              }
              return polys
            }
            const buildTopPolys = () => {
              const polys: string[] = []
              let start = -1
              const last = ucl.length - 1
              for (let i = 0; i <= last; i++) {
                const ok = isFiniteAt(ucl, i)
                if (ok && start === -1) start = i
                if ((!ok || i === last) && start !== -1) {
                  const end = ok && i === last ? i : i - 1
                  const parts: string[] = []
                  // along top edge from start to end
                  parts.push(`M${x(start).toFixed(2)},${padding.toFixed(2)}`)
                  parts.push(`L${x(end).toFixed(2)},${padding.toFixed(2)}`)
                  // back along UCL from end to start
                  for (let k = end; k >= start; k--) {
                    parts.push(`L${x(k).toFixed(2)},${y(ucl[k].value).toFixed(2)}`)
                  }
                  parts.push('Z')
                  polys.push(parts.join(' '))
                  start = -1
                }
              }
              return polys
            }
            const buildBottomPolys = () => {
              const polys: string[] = []
              let start = -1
              const last = lcl.length - 1
              for (let i = 0; i <= last; i++) {
                const ok = isFiniteAt(lcl, i)
                if (ok && start === -1) start = i
                if ((!ok || i === last) && start !== -1) {
                  const end = ok && i === last ? i : i - 1
                  const parts: string[] = []
                  // along bottom edge from start to end
                  const bottom = (height - padding).toFixed(2)
                  parts.push(`M${x(start).toFixed(2)},${bottom}`)
                  parts.push(`L${x(end).toFixed(2)},${bottom}`)
                  // back along LCL from end to start
                  for (let k = end; k >= start; k--) {
                    parts.push(`L${x(k).toFixed(2)},${y(lcl[k].value).toFixed(2)}`)
                  }
                  parts.push('Z')
                  polys.push(parts.join(' '))
                  start = -1
                }
              }
              return polys
            }

            const greenPolys = buildBandPolys()
            const topRedPolys = buildTopPolys()
            const bottomRedPolys = buildBottomPolys()

            return (
              <>
                {topRedPolys.map((d, i) => (
                  <path key={`rt-${i}`} d={d} fill="#EF4444" fillOpacity={0.06} stroke="none" />
                ))}
                {greenPolys.map((d, i) => (
                  <path key={`gn-${i}`} d={d} fill="#06B6D4" fillOpacity={0.06} stroke="none" />
                ))}
                {bottomRedPolys.map((d, i) => (
                  <path key={`rb-${i}`} d={d} fill="#EF4444" fillOpacity={0.06} stroke="none" />
                ))}
              </>
            )
          })()}
        </g>
      )}
      {/* series paths */}
      {paths}
      {/* out-of-control markers: when main > UCL or main < LCL */}
      {showOOCMarkers && series.length >= 3 && (
        <g>
          {Array.from({ length: Math.max(...series.map((s) => s.length)) }, (_, i) => {
            const mainVal = series[0]?.[i]?.value
            const lclVal = series[1]?.[i]?.value
            const uclVal = series[2]?.[i]?.value
            if (
              Number.isFinite(mainVal) &&
              Number.isFinite(lclVal) &&
              Number.isFinite(uclVal) &&
              (mainVal! > uclVal! || mainVal! < lclVal!)
            ) {
              return (
                <circle
                  key={i}
                  cx={x(i)}
                  cy={y(mainVal!)}
                  r={5.2}
                  fill="none"
                  stroke="#ff0000"
                  strokeWidth={2}
                />
              )
            }
            return null
          })}
        </g>
      )}
      {/* UCL/LCL labels at right edge near the last points */}
      {showBandLabels && series.length >= 3 && (
        <g>
          {(() => {
            // helper to find the last finite value index in a series
            const lastFiniteIndex = (s: Point[]) => {
              for (let i = s.length - 1; i >= 0; i--) {
                if (Number.isFinite(s[i]?.value)) return i
              }
              return -1
            }
            const lclSeries = series[1] || []
            const uclSeries = series[2] || []
            const li = lastFiniteIndex(lclSeries)
            const ui = lastFiniteIndex(uclSeries)
            const nodes: React.ReactNode[] = []
            if (ui >= 0) {
              const xv = Math.min(x(ui) + 6, width - padding - 2)
              const yv = y(uclSeries[ui].value)
              nodes.push(
                <text
                  key="ucl-label"
                  x={xv}
                  y={Math.max(padding + 10, yv - 6)}
                  fontSize={10}
                  fill="#374151"
                >
                  {uclLabel}
                </text>
              )
            }
            if (li >= 0) {
              const xv = Math.min(x(li) + 6, width - padding - 2)
              const yv = y(lclSeries[li].value)
              nodes.push(
                <text
                  key="lcl-label"
                  x={xv}
                  y={Math.min(height - padding - 4, Math.max(padding + 10, yv - 6))}
                  fontSize={10}
                  fill="#374151"
                >
                  {lclLabel}
                </text>
              )
            }
            return nodes
          })()}
        </g>
      )}
    </svg>
  )
}
