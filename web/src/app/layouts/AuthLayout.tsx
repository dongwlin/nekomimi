import { Space, Switch, Typography, theme } from 'antd'
import { Outlet } from 'react-router'
import { useUIStore } from '@/stores/ui.store'

export function AuthLayout() {
  const themeMode = useUIStore((state) => state.themeMode)
  const toggleTheme = useUIStore((state) => state.toggleTheme)
  const { token } = theme.useToken()

  return (
    <main
      style={{
        minHeight: '100vh',
        background: token.colorBgLayout,
        padding: 16,
      }}
    >
      <div
        style={{
          maxWidth: 1200,
          margin: '0 auto',
          display: 'flex',
          justifyContent: 'flex-end',
        }}
      >
        <Space size={6}>
          <Typography.Text type="secondary">Dark</Typography.Text>
          <Switch checked={themeMode === 'dark'} onChange={toggleTheme} />
        </Space>
      </div>

      <div
        style={{
          minHeight: 'calc(100vh - 48px)',
          display: 'grid',
          placeItems: 'center',
        }}
      >
        <Outlet />
      </div>
    </main>
  )
}
