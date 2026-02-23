import { client } from './generated/client.gen'
import { useAuthStore } from '@/features/auth/store'

const DEFAULT_API_BASE_URL = 'http://127.0.0.1:8080/api/v1'

let initialized = false

export function initApiClient() {
  if (initialized) {
    return
  }

  client.setConfig({
    baseUrl: import.meta.env.VITE_API_BASE_URL ?? DEFAULT_API_BASE_URL,
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
