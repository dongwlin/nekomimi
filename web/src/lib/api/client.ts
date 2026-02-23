import { client } from './generated/client.gen'
import { useAuthStore } from '@/features/auth/store'

const DEFAULT_API_BASE_URL = '/api/v1'

let initialized = false

export function initApiClient() {
  if (initialized) {
    return
  }

  const envBaseUrl = import.meta.env.VITE_API_BASE_URL?.trim()

  client.setConfig({
    baseUrl: envBaseUrl || DEFAULT_API_BASE_URL,
    auth: () => {
      const token = useAuthStore.getState().accessToken
      if (!token) {
        return undefined
      }
      return `Bearer ${token}`
    },
  })

  initialized = true
}
