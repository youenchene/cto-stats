import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useCycleTimes, useStocks, useStocksWeek, useThroughputWeek, usePRChangeRequestsWeek, useCloudSpendingMonthly, useCloudSpendingServices } from './api'
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
  const [activeTab, setActiveTab] = useState<'general' | 'dev' | 'cloudspending'>('general')
  return (
    <div className="min-h-full p-6 space-y-8">
      <h1 className="text-2xl font-semibold tracking-tight">{t('common.appTitle')}</h1>
      <div className="border-b mb-4">
        <div className="flex gap-4">
          <button
            className={`px-3 py-2 -mb-px border-b-2 ${
              activeTab === 'general' ? 'border-blue-600 text-blue-600' : 'border-transparent text-gray-600'
            }`}
            onClick={() => setActiveTab('general')}
          >
            {t('tabs.general')}
          </button>
          <button
            className={`px-3 py-2 -mb-px border-b-2 ${
              activeTab === 'dev' ? 'border-blue-600 text-blue-600' : 'border-transparent text-gray-600'
            }`}
            onClick={() => setActiveTab('dev')}
          >
            {t('tabs.devProcess')}
          </button>
          <button
            className={`px-3 py-2 -mb-px border-b-2 ${
              activeTab === 'cloudspending' ? 'border-blue-600 text-blue-600' : 'border-transparent text-gray-600'
            }`}
            onClick={() => setActiveTab('cloudspending')}
          >
            {t('tabs.cloudSpending')}
          </button>
        </div>
      </div>
      {activeTab === 'general' ? (
        <>
          <LeadCycleBlock />
          <StocksBlock />
          <ThroughputBlock />
        </>
      ) : activeTab === 'dev' ? (
        <>
          <DevProcessBlock />
        </>
      ) : (
        <>
          <CloudSpendingBlock />
        </>
      )}
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

function DevProcessBlock() {
  const { t } = useTranslation()
  const prWeek = usePRChangeRequestsWeek()
  const weekRows = prWeek.data ?? []

  // Stacked PR count per repo per week for the first card
  const prCountsByLabel = new Map<string, Map<string, number>>()
  const labelList: string[] = []
  for (const r of weekRows) {
    const y = r['year'] ?? ''
    const w = r['week'] ?? ''
    const repo = r['repo'] ?? ''
    if (!y || !w || !repo) continue
    const lab = `${y}-W${String(w).padStart(2, '0')}`
    if (repo === 'ALL') continue
    if (!prCountsByLabel.has(lab)) { prCountsByLabel.set(lab, new Map()); labelList.push(lab) }
    const m = prCountsByLabel.get(lab)!
    m.set(repo, (m.get(repo) ?? 0) + (parseNumber(r['cr_total']) ?? 0))
  }
  // Build stacks for repos
  const repoNames = Array.from(new Set(weekRows.map(r => r['repo']).filter(r => r && r !== 'ALL'))) as string[]
  repoNames.sort()
  const stacks: StackSeries[] = repoNames.map((name) => ({ name, values: labelList.map((lab) => prCountsByLabel.get(lab)?.get(name) ?? 0) }))


  // For the stacked chart yMax, reuse the global Stocks scale later in page
  const yMax = undefined as number | undefined

  return (
    <section>
      <h2 className="text-xl font-semibold mb-3">{t('devProcess.sectionTitle')}</h2>
      <div className="space-y-4">
        <Card>
          <CardHeader>
            <CardTitle>{t('devProcess.crStackedTitle')}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-4 gap-4 items-center">
              <div
                className="col-span-3 overflow-x-auto"
                ref={(el) => {
                  if (el) {
                    requestAnimationFrame(() => {
                      ;(el as HTMLDivElement).scrollLeft = (el as HTMLDivElement).scrollWidth
                    })
                  }
                }}
              >
                {(() => {
                  const padding = 32
                  const barGap = 6
                  const targetBarW = 12
                  const chartWidth = Math.max(900, padding * 2 + labelList.length * (targetBarW + barGap))
                  return (
                    <StackedBarChart labels={labelList} stacks={stacks} width={chartWidth} height={240} yMax={yMax} barGap={barGap} />
                  )
                })()}
              </div>
              <div className="col-span-1 flex justify-center">
                {(() => {
                  const idx = labelList.length ? labelList.length - 1 : -1
                  const total = idx >= 0 ? stacks.reduce((acc, s) => acc + (s.values[idx] || 0), 0) : null
                  return <BigNumber label={t('common.lastWeek') || 'Last week'} value={total} unit={t('devProcess.unit')} />
                })()}
              </div>
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
            <LineChart
              series={[main, lcl, ucl]}
              width={Math.max(320, containerWidth)}
              height={260}
              colors={["#000", "#9ca3af", "#9ca3af"]}
              xTickCount={5}
              yAxisTitle={t('throughput.yAxisTitle')}
              uclLabel={t('throughput.ucl')}
              lclLabel={t('throughput.lcl')}
            />
          </div>
          {/* UCL/LCL definitions under the chart */}
          <p className="mt-2 text-xs text-gray-500">
            <span className="font-medium">{t('throughput.ucl')}</span> = {t('throughput.uclFull')};{' '}
            <span className="font-medium">{t('throughput.lcl')}</span> = {t('throughput.lclFull')}
          </p>
          <p className="mt-2 text-xs text-gray-500">{t('throughput.commonCauseDescription')}</p>
            <p className="mt-2 text-xs text-gray-500">{t('throughput.specialCauseDescription')}</p>
        </CardContent>
      </Card>
    </section>
  )
}

function CloudSpendingBlock() {
  const { t } = useTranslation()
  const monthlyQuery = useCloudSpendingMonthly()
  const servicesQuery = useCloudSpendingServices()

  const monthlyData = monthlyQuery.data ?? []
  const servicesData = servicesQuery.data ?? []

  // Process overall monthly data (Azure & GCP per month)
  const monthlyLabels = useMemo(() => {
    const months = new Set<string>()
    monthlyData.forEach(r => {
      const month = r['month']
      if (month) months.add(month)
    })
    return Array.from(months).sort()
  }, [monthlyData])

  const monthlyStacks = useMemo(() => {
    const providers = new Set<string>()
    monthlyData.forEach(r => {
      const provider = r['provider']
      if (provider) providers.add(provider)
    })
    
    const colorByProvider = new Map<string, string>([
      ['azure', '#0078D4'], // Azure blue
      ['gcp', '#DB4437'],   // GCP red
      ['aws', '#FF9900'],   // AWS orange
    ])

    return Array.from(providers).sort().map(provider => {
      const values = monthlyLabels.map(month => {
        const row = monthlyData.find(r => r['month'] === month && r['provider'] === provider)
        return row ? parseNumber(row['cost']) ?? 0 : 0
      })
      return {
        name: provider.toUpperCase(),
        values,
        color: colorByProvider.get(provider)
      }
    })
  }, [monthlyData, monthlyLabels])

  // Currency detection for monthly
  const monthlyCurrency = useMemo(() => {
    const set = new Set<string>()
    monthlyData.forEach(r => {
      const c = (r['currency'] || '').toString().trim()
      if (c) set.add(c)
    })
    return set.size === 1 ? Array.from(set)[0] : ''
  }, [monthlyData])

  // Process services data (filtered services per month)
  const servicesLabels = useMemo(() => {
    const months = new Set<string>()
    servicesData.forEach(r => {
      const month = r['month']
      if (month) months.add(month)
    })
    return Array.from(months).sort()
  }, [servicesData])

  // Build a map: service/group -> { providers -> values[], currency }
  const servicesByService = useMemo(() => {
    type ProviderSeries = { name: string; values: number[]; color?: string }
    const map = new Map<string, { stacks: ProviderSeries[]; currency: string }>()
    // Use slightly different hues for the detailed per-service charts
    const colorByProvider = new Map<string, string>([
      ['azure', '#60A5FA'], // lighter Azure blue
      ['gcp',   '#EF4444'], // vivid red
      ['aws',   '#FDBA74'], // lighter AWS orange
    ])
    const tmp: Record<string, Record<string, number[]>> = {}
    const cur: Record<string, Set<string>> = {}
    servicesData.forEach(r => {
      const service = (r['group'] || r['service']) as string
      const provider = r['provider']
      const month = r['month']
      if (!service || !provider || !month) return
      const idx = servicesLabels.indexOf(month)
      if (idx < 0) return
      if (!tmp[service]) tmp[service] = {}
      if (!tmp[service][provider]) tmp[service][provider] = new Array(servicesLabels.length).fill(0)
      tmp[service][provider][idx] = parseNumber(r['cost']) ?? 0
      const c = (r['currency'] || '').toString().trim()
      if (c) {
        if (!cur[service]) cur[service] = new Set<string>()
        cur[service].add(c)
      }
    })
    Object.keys(tmp).forEach(service => {
      const stacks = Object.keys(tmp[service]).sort().map(provider => ({
        name: provider.toUpperCase(),
        values: tmp[service][provider],
        color: colorByProvider.get(provider)
      }))
      const set = cur[service]
      const currency = set && set.size === 1 ? Array.from(set)[0] : ''
      map.set(service, { stacks, currency })
    })
    return map
  }, [servicesData, servicesLabels])

  return (
    <section>
      <h2 className="text-xl font-semibold mb-3">{t('cloudSpending.sectionTitle')}</h2>
      <div className="space-y-6">
        {/* Overall costs per provider per month */}
        <Card>
          <CardHeader>
            <CardTitle>{t('cloudSpending.overallTitle')}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="w-full overflow-x-auto">
              {monthlyStacks.length > 0 ? (
                <StackedBarChart 
                  labels={monthlyLabels} 
                  stacks={monthlyStacks} 
                  width={Math.max(900, monthlyLabels.length * 60)} 
                  height={300} 
                  yAxisLabel={monthlyCurrency ? `${t('cloudSpending.amount')} (${monthlyCurrency})` : t('cloudSpending.amount')}
                  showLegend
                /> 
              ) : (
                <p className="text-gray-500">{t('cloudSpending.noDataOverall')}</p>
              )}
            </div>
          </CardContent>
        </Card>

        {/* Service-specific: one chart per service */}
        {servicesByService.size > 0 ? (
          Array.from(servicesByService.entries()).sort(([a], [b]) => a.localeCompare(b)).map(([service, info]) => (
            <Card key={service}>
              <CardHeader>
                <CardTitle>{service}</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="w-full overflow-x-auto">
                  <StackedBarChart
                    labels={servicesLabels}
                    stacks={info.stacks}
                    width={Math.max(900, servicesLabels.length * 60)}
                    height={280}
                    yAxisLabel={info.currency ? `${t('cloudSpending.amount')} (${info.currency})` : t('cloudSpending.amount')}
                    showLegend
                  />
                </div>
              </CardContent>
            </Card>
          ))
        ) : (
          <Card>
            <CardHeader>
              <CardTitle>{t('cloudSpending.perServiceTitle')}</CardTitle>
            </CardHeader>
            <CardContent>
              <p className="text-gray-500">{t('cloudSpending.noDataServices')}</p>
            </CardContent>
          </Card>
        )}
      </div>
    </section>
  )
}
