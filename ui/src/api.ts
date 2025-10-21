import { useQuery } from '@tanstack/react-query'

export type Row = Record<string, string>

async function fetchJSON<T>(url: string): Promise<T> {
  const res = await fetch(url)
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  return res.json()
}

export function useCycleTimes() {
  return useQuery<Row[]>({
    queryKey: ['cycle_times'],
    queryFn: () => fetchJSON('/api/cycle_times'),
  })
}

export function useStocksWeek() {
  return useQuery<Row[]>({
    queryKey: ['stocks_week'],
    queryFn: () => fetchJSON('/api/stocks/week'),
  })
}

export function useStocks() {
  return useQuery<Row[]>({
    queryKey: ['stocks'],
    queryFn: () => fetchJSON('/api/stocks'),
  })
}

export function useThroughputMonth() {
  return useQuery<Row[]>({
    queryKey: ['throughput_month'],
    queryFn: () => fetchJSON('/api/throughtput/month'),
    retry: (failureCount, error: any) => {
      // If endpoint missing (404), don't retry endlessly
      return !(error?.message?.startsWith?.('404')) && failureCount < 2
    }
  })
}

export function useThroughputWeek() {
  return useQuery<Row[]>({
    queryKey: ['throughput_week'],
    queryFn: () => fetchJSON('/api/throughtput/week'),
  })
}
