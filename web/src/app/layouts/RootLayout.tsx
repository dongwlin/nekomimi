import { Button, Layout, Space, Switch, Typography, theme } from 'antd'
import { Outlet, useLocation, useNavigate } from 'react-router'
import { useAuthStore } from '@/features/auth/store'
import { useUIStore } from '@/stores/ui.store'

const { Header, Content } = Layout

export function RootLayout() {
  const location = useLocation()
  const navigate = useNavigate()
  const clearTokens = useAuthStore((state) => state.clearTokens)
  const themeMode = useUIStore((state) => state.themeMode)
  const toggleTheme = useUIStore((state) => state.toggleTheme)
  const { token } = theme.useToken()

  const dashboardActive = location.pathname.startsWith('/dashboard')
  const settingsActive = location.pathname.startsWith('/settings')

  return (
    <Layout style={{ minHeight: '100vh', background: token.colorBgLayout }}>
      <Header
        style={{
          position: 'sticky',
          top: 0,
          zIndex: 10,
          height: 'auto',
          lineHeight: 'normal',
          background: token.colorBgContainer,
          borderBottom: `1px solid ${token.colorBorderSecondary}`,
          padding: '12px 24px',
        }}
      >
        <div
          style={{
            maxWidth: 1200,
            margin: '0 auto',
            display: 'flex',
            alignItems: 'center',
            gap: 12,
            flexWrap: 'wrap',
          }}
        >
          <Typography.Title level={5} style={{ margin: 0 }}>
            Nekomimi Admin
          </Typography.Title>

          <Space size={8}>
            <Button type={dashboardActive ? 'primary' : 'text'} onClick={() => navigate('/dashboard')}>
              Dashboard
            </Button>
            <Button type={settingsActive ? 'primary' : 'text'} onClick={() => navigate('/settings/security')}>
              Settings
            </Button>
          </Space>

          <Space size={12} style={{ marginLeft: 'auto' }}>
            <Space size={6}>
              <Typography.Text type="secondary">Dark</Typography.Text>
              <Switch checked={themeMode === 'dark'} onChange={toggleTheme} />
            </Space>
            <Button
              onClick={() => {
                clearTokens()
                void navigate('/login', { replace: true })
              }}
            >
              Logout
            </Button>
          </Space>
        </div>
      </Header>

      <Content
        style={{
          width: 'min(1200px, 100%)',
          margin: '24px auto 0',
          padding: '0 16px 24px',
        }}
      >
        <Outlet />
      </Content>
    </Layout>
  )
}
