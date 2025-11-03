import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useCycleTimes, useStocks, useStocksWeek, useThroughputWeek } from './api'
import { Card, CardContent, CardHeader, CardTitle } from './components/ui/card'
import { Sparkline } from './components/Sparkline'
import { LineChart, Point } from './components/LineChart'
import { StackedBarChart, StackSeries } from './components/StackedBarChart'
import { parseNumber } from './lib/utils'
import { useTranslation } from 'react-i18next'

function BigNumber({ label, value, unit }: { label: string; value: number | null; unit?: string }) {
  const hasValue = value != null
  return (
    <div className="flex flex-col">
      <div className="text-sm text-gray-500 mb-1">{label}</div>
      <div className="text-4xl font-bold flex items-baseline gap-2">
        <span>{hasValue ? value!.toFixed(0) : '—'}</span>
        {hasValue && unit ? <span className="text-sm text-gray-500">{unit}</span> : null}
      </div>
    </div>
  )
}

export default function App() {
  const { t } = useTranslation()
  return (
    <div className="min-h-full p-6 space-y-8">
      <h1 className="text-2xl font-semibold tracking-tight">{t('common.appTitle')}</h1>
      <LeadCycleBlock />
      <StocksBlock />
      <ThroughputBlock />
    </div>
  )
}

function LeadCycleBlock() {
  const { t } = useTranslation()
  const { data } = useCycleTimes()
  const points = (data ?? []).map((r) => ({
    label: r[
      Object.keys(r).find((k) => {
        const lk = k.toLowerCase()
        return lk.includes('year') || lk.includes('month')
      }) || ''
    ] ?? '',
    // Support multiple possible CSV headers
    lead: parseNumber(
      r['leadtime_days_avg'] ?? r['lead_time'] ?? r['lead'] ?? r['leadtime']
    ),
    cycle: parseNumber(
      r['cycletime_days_avg'] ?? r['cycle_time'] ?? r['cycle'] ?? r['cycletime']
    ),
    timeToPR: parseNumber(
      r['time_to_pr'] ?? r['time_to_pr_days'] ?? r['timeto_pr'] ?? r['time_to_pr_avg']
    ),
  }))
  const leadSeries = points.map((p) => p.lead ?? 0)
  const cycleSeries = points.map((p) => p.cycle ?? 0)
  const tprSeries = points.map((p) => p.timeToPR ?? 0)
  const leadLast = points.length ? points[points.length - 1].lead : null
  const cycleLast = points.length ? points[points.length - 1].cycle : null
  const tprLast = points.length ? points[points.length - 1].timeToPR : null
  return (
    <section>
      <h2 className="text-xl font-semibold mb-3">{t('leadCycle.sectionTitle')}</h2>
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <Card>
          <CardHeader>
            <CardTitle>{t('leadCycle.leadCardTitle')}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-end justify-between">
              <Sparkline data={leadSeries} width={300} />
              <BigNumber label={t('common.current')} value={leadLast} unit={t('units.days')} />
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>{t('leadCycle.cycleCardTitle')}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-end justify-between">
              <Sparkline data={cycleSeries} width={300} />
              <BigNumber label={t('common.current')} value={cycleLast} unit={t('units.days')} />
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>{t('leadCycle.timeToPRCardTitle')}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-end justify-between">
              <Sparkline data={tprSeries} width={300} />
              <BigNumber label={t('common.current')} value={tprLast} unit={t('units.days')} />
            </div>
          </CardContent>
        </Card>
      </div>
    </section>
  )
}

function StocksBlock() {
  const { t } = useTranslation()
  // Use weekly endpoint to build stacked bars per project per week
  const week = useStocksWeek()
  const rows = week.data ?? []

  // Build ordered unique labels of the form YYYY-WW
  const labelKey = (r: Record<string, string>) => {
    const y = r['year'] ?? ''
    const w = r['week'] ?? (r['year_week'] ?? r['year-week'] ?? r['week'] ?? '')
    return y && w ? `${y}-W${String(w).padStart(2, '0')}` : String(w)
  }
  const labelList: string[] = []
  const groups = new Map<string, Record<string, string>[]>()
  for (const r of rows) {
    const k = labelKey(r)
    if (!groups.has(k)) {
      groups.set(k, [])
      labelList.push(k)
    }
    groups.get(k)!.push(r)
  }

  // Collect project names
  const projectNames: string[] = Array.from(
    new Set(
      rows.map((r) => (r['project_name']?.trim() ? r['project_name'].trim() : t('stocks.unassigned')))
    )
  )

  function stacksFor(key: string): { labels: string[]; stacks: StackSeries[] } {
    const stacks: StackSeries[] = projectNames.map((name) => ({ name, values: Array(labelList.length).fill(0) }))
    labelList.forEach((lab, idx) => {
      const rs = groups.get(lab) ?? []
      // sum value per project for this week label
      for (const r of rs) {
        const name = r['project_name']?.trim() ? r['project_name'].trim() : t('stocks.unassigned')
        const v = parseNumber(r[key]) ?? 0
        const s = stacks.find((t0) => t0.name === name)!
        s.values[idx] += v
      }
    })
    return { labels: labelList, stacks }
  }

  // Big numbers: use /api/stocks (current snapshot), summed across projects
  const stocks = useStocks()
  const sumFor = (key: string): number | null => {
    const rs = stocks.data ?? []
    if (!rs.length) return null
    return rs.reduce((acc, r) => acc + (parseNumber(r[key]) ?? 0), 0)
  }

  const items: { key: string; labelKey: string }[] = [
    { key: 'opened_bugs', labelKey: 'opened_bugs' },
    { key: 'waiting_to_prod', labelKey: 'waiting_to_prod' },
    { key: 'in_qa', labelKey: 'in_qa' },
    { key: 'in_review', labelKey: 'in_review' },
    { key: 'in_dev', labelKey: 'in_dev' },
    { key: 'in_ready', labelKey: 'in_ready' },
    { key: 'in_backlogs', labelKey: 'in_backlogs' },
  ]

  // Compute a global Y max across all stacked charts so they share the same scale
  const globalYMax = useMemo(() => {
    let maxVal = 1
    for (const { key } of items) {
      const { stacks } = stacksFor(key)
      // totals per bar (per label)
      for (let i = 0; i < labelList.length; i++) {
        const total = stacks.reduce((acc, s) => acc + (s.values[i] || 0), 0)
        if (total > maxVal) maxVal = total
      }
    }
    return maxVal
  }, [rows, projectNames, labelList])

  return (
    <section>
      <h2 className="text-xl font-semibold mb-3">{t('stocks.sectionTitle')}</h2>
      <div className="space-y-4">
        {items.map(({ key, labelKey }) => {
          const { labels, stacks } = stacksFor(key)
          const total = sumFor(key)
          return (
            <Card key={key}>
              <CardHeader>
                <CardTitle>{t(`stocks.labels.${labelKey}`)}</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="grid grid-cols-4 gap-4 items-center">
                  <div
                    className="col-span-3 overflow-x-auto"
                    ref={(el) => {
                      if (el) {
                        // Scroll to the far right when the element mounts or updates
                        requestAnimationFrame(() => {
                          ;(el as HTMLDivElement).scrollLeft = el.scrollWidth
                        })
                      }
                    }}
                  >
                    <StackedBarChart labels={labels} stacks={stacks} width={900} height={220} yMax={globalYMax} />
                  </div>
                  <div className="col-span-1 flex justify-center">
                    <BigNumber label={t('common.current')} value={total} unit={t('units.issues')} />
                  </div>
                </div>
              </CardContent>
            </Card>
          )
        })}
      </div>
    </section>
  )
}

function ThroughputBlock() {
  const { t } = useTranslation()
  const query = useThroughputWeek()

  const data = query.data ?? []
  const labels = data.map((r) => {
    const y = r['year']
    const w = r['week']
    if (y && w) return `${y}-W${String(w).padStart(2, '0')}`
    return (
      r['year_week'] ??
      r['year-week'] ??
      r['year-month'] ??
      r['year_month'] ??
      (w ? String(w) : '')
    )
  })

  const main: Point[] = data.map((r, i) => ({ label: labels[i], value: parseNumber(r['throughput'] ?? r['main'] ?? r['value']) ?? 0 }))
  const lcl: Point[] = data.map((r, i) => ({ label: labels[i], value: parseNumber(r['lcl']) ?? 0 }))
  const ucl: Point[] = data.map((r, i) => ({ label: labels[i], value: parseNumber(r['ucl']) ?? 0 }))

  // Responsive width: measure container width and update on resize
  const containerRef = useRef<HTMLDivElement>(null)
  const [containerWidth, setContainerWidth] = useState<number>(800)
  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const update = () => setContainerWidth(el.clientWidth)
    update()
    const ro = new ResizeObserver(() => update())
    ro.observe(el)
    window.addEventListener('resize', update)
    return () => {
      ro.disconnect()
      window.removeEventListener('resize', update)
    }
  }, [])

  return (
    <section>
      <h2 className="text-xl font-semibold mb-3">{t('throughput.sectionTitle')}</h2>
      <Card>
        <CardContent>
          <div ref={containerRef} className="w-full">
            <LineChart series={[main, lcl, ucl]} width={Math.max(320, containerWidth)} height={260} colors={["#000", "#9ca3af", "#9ca3af"]} xTickCount={5} />
          </div>
        </CardContent>
      </Card>
    </section>
  )
}
