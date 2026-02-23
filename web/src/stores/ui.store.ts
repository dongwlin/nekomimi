import { create } from 'zustand'
import { createJSONStorage, persist } from 'zustand/middleware'

const UI_STORAGE_KEY = 'nekomimi.web.ui'

export type ThemeMode = 'light' | 'dark'

interface UIStore {
  themeMode: ThemeMode
  setThemeMode: (themeMode: ThemeMode) => void
  toggleTheme: () => void
}

export const useUIStore = create<UIStore>()(
  persist(
    (set) => ({
      themeMode: 'light',
      setThemeMode: (themeMode) => {
        set({ themeMode })
      },
      toggleTheme: () => {
        set((state) => ({
          themeMode: state.themeMode === 'dark' ? 'light' : 'dark',
        }))
      },
    }),
    {
      name: UI_STORAGE_KEY,
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({
        themeMode: state.themeMode,
      }),
    },
  ),
)
