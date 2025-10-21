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


export function useThroughputWeek() {
  return useQuery<Row[]>({
    queryKey: ['throughput_week'],
    queryFn: () => fetchJSON('/api/throughput/week'),
  })
}
