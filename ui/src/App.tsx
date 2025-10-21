import React from 'react'
import { useCycleTimes, useStocksWeek, useThroughputMonth, useThroughputWeek } from './api'
import { Card, CardContent, CardHeader, CardTitle } from './components/ui/card'
import { Sparkline } from './components/Sparkline'
import { LineChart, Point } from './components/LineChart'
import { parseNumber } from './lib/utils'

function BigNumber({ label, value }: { label: string; value: number | null }) {
  return (
    <div className="flex flex-col">
      <div className="text-sm text-gray-500 mb-1">{label}</div>
      <div className="text-4xl font-bold">{value != null ? value.toFixed(0) : '—'}</div>
    </div>
  )
}

export default function App() {
  return (
    <div className="min-h-full p-6 space-y-8">
      <h1 className="text-2xl font-semibold tracking-tight">Dashboard</h1>
      <LeadCycleBlock />
      <StocksBlock />
      <ThroughputBlock />
    </div>
  )
}

function LeadCycleBlock() {
  const { data } = useCycleTimes()
  const points = (data ?? []).map((r) => ({
    label: r[Object.keys(r).find((k) => k.toLowerCase().includes('year')) || ''] ?? '',
    lead: parseNumber(r['lead_time'] ?? r['lead'] ?? r['leadtime']),
    cycle: parseNumber(r['cycle_time'] ?? r['cycle'] ?? r['cycletime']),
  }))
  const leadSeries = points.map((p) => p.lead ?? 0)
  const cycleSeries = points.map((p) => p.cycle ?? 0)
  const leadLast = points.length ? points[points.length - 1].lead : null
  const cycleLast = points.length ? points[points.length - 1].cycle : null
  return (
    <section>
      <h2 className="text-xl font-semibold mb-3">Lead & Cycle Times</h2>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <Card>
          <CardHeader>
            <CardTitle>Lead Time (days)</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-end justify-between">
              <BigNumber label="Current" value={leadLast} />
              <Sparkline data={leadSeries} width={300} />
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Cycle Time (days)</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-end justify-between">
              <BigNumber label="Current" value={cycleLast} />
              <Sparkline data={cycleSeries} width={300} />
            </div>
          </CardContent>
        </Card>
      </div>
    </section>
  )
}

function StocksBlock() {
  const { data } = useStocksWeek()
  const rows = data ?? []
  const labels = rows.map((r) => r['year_week'] ?? r['year-week'] ?? r['week'] ?? '')

  function seriesFor(key: string): Point[] {
    return rows.map((r) => ({ label: labels[rows.indexOf(r)], value: parseNumber(r[key]) ?? 0 }))
  }

  const items: { key: string; label: string }[] = [
    { key: 'opened_bugs', label: 'Red bin (opened_bugs)' },
    { key: 'waiting_to_prod', label: 'waiting_to_prod' },
    { key: 'in_review', label: 'in_review' },
    { key: 'in_qa', label: 'in_qa' },
    { key: 'in_dev', label: 'in_dev' },
    { key: 'in_backlogs', label: 'in_backlogs' },
  ]

  return (
    <section>
      <h2 className="text-xl font-semibold mb-3">Stocks</h2>
      <div className="space-y-4">
        {items.map(({ key, label }) => {
          const s = seriesFor(key)
          const last = s.length ? s[s.length - 1].value : null
          return (
            <Card key={key}>
              <CardHeader>
                <CardTitle>{label}</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="grid grid-cols-4 gap-4 items-center">
                  <div className="col-span-3 overflow-x-auto">
                    <LineChart series={[s]} width={900} height={220} colors={["#000"]} />
                  </div>
                  <div className="col-span-1 flex justify-center">
                    <BigNumber label="Current" value={last} />
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
  const month = useThroughputMonth()
  const week = useThroughputWeek()

  const data = month.data?.length ? month.data : week.data ?? []
  const labels = data.map((r) => r['year_week'] ?? r['year-month'] ?? r['year_month'] ?? '')

  const main: Point[] = data.map((r, i) => ({ label: labels[i], value: parseNumber(r['throughput'] ?? r['main'] ?? r['value']) ?? 0 }))
  const lcl: Point[] = data.map((r, i) => ({ label: labels[i], value: parseNumber(r['lcl']) ?? 0 }))
  const ucl: Point[] = data.map((r, i) => ({ label: labels[i], value: parseNumber(r['ucl']) ?? 0 }))

  return (
    <section>
      <h2 className="text-xl font-semibold mb-3">Throughput</h2>
      <Card>
        <CardContent>
          <LineChart series={[main, lcl, ucl]} width={1100} height={260} colors={["#000", "#9ca3af", "#9ca3af"]} />
        </CardContent>
      </Card>
    </section>
  )
}
