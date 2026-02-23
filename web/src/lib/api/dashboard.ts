import { dashboardOverview } from './generated'
import type { MetricsOverview } from './generated'
import { initApiClient } from './client'
import { withAutoRefresh } from './auth'

interface APIResult<TData = unknown, TError = unknown> {
  data?: TData
  error?: TError
  request: Request
  response: Response
}

export async function getDashboardOverview(): Promise<MetricsOverview> {
  initApiClient()

  const result = (await withAutoRefresh(
    async () => dashboardOverview() as Promise<APIResult<MetricsOverview, { error?: string }>>,
  )) as APIResult<MetricsOverview, { error?: string }>

  if (result.response.status !== 200 || !result.data) {
    const message = result.error?.error ?? 'dashboard_overview_failed'
    throw new Error(message)
  }
  return result.data
}
