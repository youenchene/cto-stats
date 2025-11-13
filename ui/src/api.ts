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

export function usePRChangeRequestsWeek() {
  return useQuery<Row[]>({
    queryKey: ['pr_change_requests_week'],
    queryFn: () => fetchJSON('/api/pr/change_requests'),
  })
}

export function usePRChangeRequestsRepo() {
  return useQuery<Row[]>({
    queryKey: ['pr_change_requests_repo'],
    queryFn: () => fetchJSON('/api/pr/change_requests/repo'),
  })
}

export function usePRChangeRequestsRepoDist() {
  return useQuery<Row[]>({
    queryKey: ['pr_change_requests_repo_dist'],
    queryFn: () => fetchJSON('/api/pr/change_requests/repo_dist'),
  })
}
