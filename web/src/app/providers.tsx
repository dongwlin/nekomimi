import { useEffect, useMemo, type PropsWithChildren } from 'react'
import { App as AntdApp, ConfigProvider, theme, type ThemeConfig } from 'antd'
import { useUIStore } from '@/stores/ui.store'

export function AppProviders({ children }: PropsWithChildren) {
  const themeMode = useUIStore((state) => state.themeMode)

  const appTheme = useMemo<ThemeConfig>(() => {
    return {
      algorithm: themeMode === 'dark' ? theme.darkAlgorithm : theme.defaultAlgorithm,
      token: {
        borderRadius: 10,
        colorPrimary: '#0d9488',
      },
    }
  }, [themeMode])

  useEffect(() => {
    document.documentElement.style.colorScheme = themeMode
    document.body.style.backgroundColor = themeMode === 'dark' ? '#111827' : '#f5f5f5'
  }, [themeMode])

  return (
    <ConfigProvider theme={appTheme}>
      <AntdApp>{children}</AntdApp>
    </ConfigProvider>
  )
}
