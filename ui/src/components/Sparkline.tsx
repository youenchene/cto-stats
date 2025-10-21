import React from 'react'

export function Sparkline({ data, width = 240, height = 48, stroke = '#000' }: { data: number[]; width?: number; height?: number; stroke?: string }) {
  const valid = data.filter((d) => Number.isFinite(d))
  if (valid.length === 0) return <svg width={width} height={height} />
  const min = Math.min(...valid)
  const max = Math.max(...valid)
  const rng = max - min || 1
  const step = width / Math.max(data.length - 1, 1)
  const path = data
    .map((d, i) => {
      const x = i * step
      const y = height - ((d - min) / rng) * (height - 2) - 1
      return `${i === 0 ? 'M' : 'L'}${x.toFixed(2)},${y.toFixed(2)}`
    })
    .join(' ')

  return (
    <svg width={width} height={height} className="chart-grayscale">
      <polyline points={`0,${height} ${width},${height}`} fill="none" stroke="#e5e7eb" strokeWidth={1} />
      <path d={path} fill="none" stroke={stroke} strokeWidth={2} />
    </svg>
  )
}
