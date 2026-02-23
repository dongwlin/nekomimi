import { create } from 'zustand'
import { createJSONStorage, persist } from 'zustand/middleware'

const AUTH_STORAGE_KEY = 'nekomimi.web.auth-token'

interface AuthStore {
  accessToken: string | null
  refreshToken: string | null
  setTokens: (accessToken: string, refreshToken: string) => void
  clearTokens: () => void
}

export const useAuthStore = create<AuthStore>()(
  persist(
    (set) => ({
      accessToken: null,
      refreshToken: null,
      setTokens: (accessToken, refreshToken) => {
        set({ accessToken, refreshToken })
      },
      clearTokens: () => {
        set({ accessToken: null, refreshToken: null })
      },
    }),
    {
      name: AUTH_STORAGE_KEY,
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({
        accessToken: state.accessToken,
        refreshToken: state.refreshToken,
      }),
    },
  ),
)
